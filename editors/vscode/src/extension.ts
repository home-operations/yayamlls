import * as fs from "fs/promises";
import {
  commands,
  ExtensionContext,
  LogOutputChannel,
  window,
  workspace,
  WorkspaceConfiguration,
} from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";
import { ensureBinary } from "./download";

const YAMLLS_REPO = "home-operations/yayamlls";

let client: LanguageClient | undefined;
let output: LogOutputChannel;

function explicit<T>(cfg: WorkspaceConfiguration, key: string): T | undefined {
  const info = cfg.inspect<T>(key);
  if (!info) {
    return undefined;
  }
  if (
    info.globalValue !== undefined ||
    info.workspaceValue !== undefined ||
    info.workspaceFolderValue !== undefined
  ) {
    return cfg.get<T>(key);
  }
  return undefined;
}

/** Build settings from `yayamlls.*` config, including only explicitly-set values. */
function buildSettings() {
  const cfg = workspace.getConfiguration("yayamlls");
  const opts: Record<string, unknown> = {};
  const catalog = explicit<boolean>(cfg, "catalog");
  if (catalog !== undefined) {
    opts.catalog = catalog;
  }
  const schemas = explicit<object>(cfg, "schemas");
  if (schemas !== undefined) {
    opts.schemas = schemas;
  }
  const catalogUrl = explicit<string>(cfg, "catalogUrl");
  if (catalogUrl !== undefined) {
    opts.catalogUrl = catalogUrl;
  }
  const schemaUrl = explicit<string>(cfg, "kubernetes.schemaUrl");
  if (schemaUrl !== undefined) {
    opts.kubernetes = { schemaUrl };
  }
  const flateEnabled = explicit<boolean>(cfg, "flate.enabled");
  if (flateEnabled !== undefined) {
    opts.renderers = { flate: { enabled: flateEnabled } };
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
    initializationOptions: buildSettings(),
    outputChannel: output,
    middleware: {
      workspace: {
        configuration: async (params) => {
          const result: unknown[] = [];
          for (const item of params.items) {
            if (item.section === "yayamlls") {
              result.push(buildSettings());
            } else {
              result.push(null);
            }
          }
          return result;
        },
      },
    },
  };
  client = new LanguageClient(
    "yayamlls",
    "yayamlls",
    serverOptions,
    clientOptions,
  );
  await client.start();

  // Push settings to the server when yayamlls.* config changes.
  context.subscriptions.push(
    workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration("yayamlls") || !client) return;
      client.sendNotification("workspace/didChangeConfiguration", {
        settings: buildSettings(),
      });
    }),
  );
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
