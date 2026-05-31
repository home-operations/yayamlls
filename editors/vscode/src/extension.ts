import * as fs from "fs/promises";
import * as path from "path";
import {
  commands,
  ExtensionContext,
  OutputChannel,
  window,
  workspace,
} from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";
import { ensureBinary } from "./download";

const YAMLLS_REPO = "home-operations/yayamlls";
const FLATE_REPO = "home-operations/flate";

let client: LanguageClient | undefined;
let output: OutputChannel;

/** Build the server's initializationOptions from `yayamlls.*` settings. */
function initializationOptions() {
  const cfg = workspace.getConfiguration("yayamlls");
  const opts: Record<string, unknown> = {
    catalog: cfg.get<boolean>("catalog", true),
    schemas: cfg.get<object>("schemas", {}),
  };
  const catalogUrl = cfg.get<string>("catalogUrl", "");
  if (catalogUrl) {
    opts.catalogUrl = catalogUrl;
  }
  const schemaUrl = cfg.get<string>("kubernetes.schemaUrl", "");
  if (schemaUrl) {
    opts.kubernetes = { schemaUrl };
  }
  // The flate binary is exposed to the server via PATH (see startClient), not
  // here, so it doesn't clobber renderers.flate settings from .yayamlls.yaml.
  if (!cfg.get<boolean>("flate.enabled", true)) {
    opts.renderers = { flate: { enabled: false } };
  }
  return opts;
}

/** Resolve the flate binary for Flux rendering, downloading it if enabled. */
async function resolveFlate(storageDir: string): Promise<string | undefined> {
  const cfg = workspace.getConfiguration("yayamlls");
  if (!cfg.get<boolean>("flate.enabled", true)) {
    return undefined;
  }
  const override = cfg.get<string>("flate.path", "").trim();
  if (override) {
    return override;
  }
  try {
    return await ensureBinary(
      storageDir,
      FLATE_REPO,
      "flate",
      cfg.get<string>("flate.version", "latest"),
      output,
    );
  } catch (err) {
    // Rendering is optional; degrade gracefully if flate can't be fetched.
    output.appendLine(`yayamlls: flate unavailable, rendering disabled (${err})`);
    return undefined;
  }
}

async function resolveCommand(storageDir: string): Promise<string> {
  const cfg = workspace.getConfiguration("yayamlls");
  const override = cfg.get<string>("path", "").trim();
  if (override) {
    return override;
  }
  return ensureBinary(storageDir, YAMLLS_REPO, "yayamlls", cfg.get<string>("version", "latest"), output);
}

async function startClient(context: ExtensionContext): Promise<void> {
  const storageDir = context.globalStorageUri.fsPath;
  await fs.mkdir(storageDir, { recursive: true });

  const command = await resolveCommand(storageDir);
  const flatePath = await resolveFlate(storageDir);
  // Communication defaults to stdio; leaving transport unset avoids appending
  // a --stdio flag the server doesn't parse.
  const serverOptions: ServerOptions = {
    command,
  };
  // Expose flate on the server's PATH rather than via a renderers.flate.binary
  // override, so the server's own renderers config (e.g. flate.path in
  // .yayamlls.yaml) is preserved.
  if (flatePath) {
    const dir = path.dirname(flatePath);
    if (dir && dir !== ".") {
      serverOptions.options = {
        env: { ...process.env, PATH: `${dir}${path.delimiter}${process.env.PATH ?? ""}` },
      };
    }
  }
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "yaml" }],
    initializationOptions: initializationOptions(),
    synchronize: { configurationSection: "yayamlls" },
    outputChannel: output,
  };
  client = new LanguageClient("yayamlls", "yayamlls", serverOptions, clientOptions);
  await client.start();
}

export async function activate(context: ExtensionContext): Promise<void> {
  output = window.createOutputChannel("yayamlls");
  context.subscriptions.push(output);

  // The language client auto-registers the server's executeCommand provider
  // commands (yayamlls.showRendered*) and forwards the code lens's URI
  // argument; the server renders and opens the result via window/showDocument.
  // So only the client-side restart command lives here.
  context.subscriptions.push(
    commands.registerCommand("yayamlls.restart", async () => {
      await client?.stop();
      client = undefined;
      await startClient(context).catch((err) =>
        window.showErrorMessage(`yayamlls failed to start: ${err}`),
      );
    }),
  );

  try {
    await startClient(context);
  } catch (err) {
    window.showErrorMessage(`yayamlls failed to start: ${err}`);
  }
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
