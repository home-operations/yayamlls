import { createHash } from "crypto";
import * as fs from "fs/promises";
import * as os from "os";
import * as path from "path";
import { execFile } from "child_process";
import { promisify } from "util";
import { OutputChannel } from "vscode";

const execFileP = promisify(execFile);

/** Maps Node's platform/arch to a GoReleaser asset + the binary inside it. */
function assetFor(name: string, version: string): { asset: string; binary: string } {
  const osPart =
    process.platform === "darwin" ? "darwin" : process.platform === "win32" ? "windows" : "linux";
  const archPart = process.arch === "arm64" ? "arm64" : process.arch === "x64" ? "amd64" : "";
  if (!archPart) {
    throw new Error(`${name} has no build for arch "${process.arch}"`);
  }
  if (osPart === "windows" && archPart === "arm64") {
    throw new Error(`${name} has no windows/arm64 build`);
  }
  const ext = osPart === "windows" ? "zip" : "tar.gz";
  return {
    asset: `${name}_${version}_${osPart}_${archPart}.${ext}`,
    binary: osPart === "windows" ? `${name}.exe` : name,
  };
}

// Release tags carry no leading "v"; one is tolerated and stripped. The result
// ends up in file paths and URLs, so anything else is rejected.
const VERSION_RE = /^\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$/;

function validVersion(raw: string): string {
  const version = raw.replace(/^v/, "");
  if (!VERSION_RE.test(version)) {
    throw new Error(`invalid release version "${raw}"`);
  }
  return version;
}

async function resolveVersion(repo: string, requested: string): Promise<string> {
  if (requested && requested !== "latest") {
    return validVersion(requested);
  }
  const res = await fetch(`https://api.github.com/repos/${repo}/releases/latest`, {
    headers: { Accept: "application/vnd.github+json", "User-Agent": "yayamlls-vscode" },
  });
  if (!res.ok) {
    throw new Error(`GitHub API ${res.status} resolving latest ${repo} release`);
  }
  const body = (await res.json()) as { tag_name?: string };
  if (!body.tag_name) {
    throw new Error(`latest ${repo} release has no tag_name`);
  }
  return validVersion(body.tag_name);
}

async function fetchBytes(url: string): Promise<Buffer> {
  const res = await fetch(url, { headers: { "User-Agent": "yayamlls-vscode" } });
  if (!res.ok) {
    throw new Error(`download failed: ${res.status} ${url}`);
  }
  return Buffer.from(await res.arrayBuffer());
}

async function exists(p: string): Promise<boolean> {
  try {
    await fs.access(p);
    return true;
  } catch {
    return false;
  }
}

/**
 * Returns a path to a usable binary from a GoReleaser-published repo, downloading
 * the matching release asset into `storageDir` on first use (or version bump).
 *
 * @param repo  GitHub "owner/name"
 * @param name  binary/project name (also the asset prefix, e.g. "yayamlls" / "flate")
 */
export async function ensureBinary(
  storageDir: string,
  repo: string,
  name: string,
  versionSetting: string,
  out: OutputChannel,
): Promise<string> {
  const version = await resolveVersion(repo, versionSetting);
  const { asset, binary } = assetFor(name, version);

  const versionDir = path.join(storageDir, `${name}-${version}`);
  const binaryPath = path.join(versionDir, binary);
  if (await exists(binaryPath)) {
    return binaryPath;
  }

  out.appendLine(`yayamlls: downloading ${asset} (${version})`);
  await fs.mkdir(versionDir, { recursive: true });

  const url = `https://github.com/${repo}/releases/download/${version}/${asset}`;
  const archive = await fetchBytes(url);
  // GoReleaser's split checksum file: "<hex sha256>  <asset>".
  const expected = (await fetchBytes(`${url}.sha256`)).toString("utf8").trim().split(/\s+/)[0];
  const actual = createHash("sha256").update(archive).digest("hex");
  if (!expected || actual !== expected.toLowerCase()) {
    throw new Error(`checksum mismatch for ${asset}: got ${actual}, expected ${expected}`);
  }

  const tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "yayamlls-"));
  try {
    const archivePath = path.join(tmpDir, asset);
    await fs.writeFile(archivePath, archive);
    // `tar` ships on macOS, Linux, and Windows 10 1803+ (bsdtar handles .zip too),
    // so a single invocation extracts every asset type without extra deps.
    await execFileP("tar", ["-xf", archivePath, "-C", versionDir]);
  } finally {
    await fs.rm(tmpDir, { recursive: true, force: true });
  }

  if (process.platform !== "win32") {
    await fs.chmod(binaryPath, 0o755);
  }
  if (!(await exists(binaryPath))) {
    throw new Error(`extracted archive but ${binary} is missing in ${versionDir}`);
  }

  // Drop stale version directories for this binary.
  for (const entry of await fs.readdir(storageDir)) {
    if (entry.startsWith(`${name}-`) && entry !== `${name}-${version}`) {
      await fs.rm(path.join(storageDir, entry), { recursive: true, force: true });
    }
  }

  out.appendLine(`yayamlls: installed ${binaryPath}`);
  return binaryPath;
}
