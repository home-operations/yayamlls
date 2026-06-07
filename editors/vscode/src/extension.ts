import * as fs from "fs/promises";
import { commands, ExtensionContext, LogOutputChannel, window, workspace } from "vscode";
import { LanguageClient, LanguageClientOptions, ServerOptions } from "vscode-languageclient/node";
import { ensureBinary } from "./download";

const YAMLLS_REPO = "home-operations/yayamlls";

let client: LanguageClient | undefined;
let output: LogOutputChannel;

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
  if (!cfg.get<boolean>("flate.enabled", true)) {
    opts.renderers = { flate: { enabled: false } };
  }
  return opts;
}

async function resolveCommand(storageDir: string): Promise<string> {
  const cfg = workspace.getConfiguration("yayamlls");
  const override = cfg.get<string>("path", "").trim();
  if (override) {
    return override;
  }
  return ensureBinary(
    storageDir,
    YAMLLS_REPO,
    "yayamlls",
    cfg.get<string>("version", "latest"),
    output,
  );
}

async function startClient(context: ExtensionContext): Promise<void> {
  const storageDir = context.globalStorageUri.fsPath;
  await fs.mkdir(storageDir, { recursive: true });

  const command = await resolveCommand(storageDir);
  // No transport: stdio is the default. Setting it makes the client append a
  // `--stdio` arg the server's flag parser rejects.
  const serverOptions: ServerOptions = {
    command,
  };
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
  output = window.createOutputChannel("yayamlls", { log: true });
  context.subscriptions.push(output);

  // showRendered/showRenderedDiff are auto-registered from the server's
  // executeCommand capabilities; registering them here too would collide.
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
