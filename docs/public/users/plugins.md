# Using plugins

Plugins extend Cascade with extra commands and features, such as coloring
nicknames or reacting to events. They're small standalone programs that Cascade
discovers automatically; there's no app store or manual "add" step.

## Installing a plugin

Cascade discovers plugins from two places:

- **`~/.cascade-chat/plugins/`** — drop the plugin's executable (or a folder
  containing it) here.
- **Your `PATH`** — any executable named `cascade-*` is picked up automatically.

!!! note "Naming"
    Plugins are identified by the `cascade-` prefix. A program called
    `cascade-nickname-colors` shows up as the nickname-colors plugin.

New plugins are discovered when Cascade starts. You can also re-scan without
restarting from the Plugins settings (see below).

## Managing plugins

Open **Settings → Plugins**. Each discovered plugin shows its name, version,
author, description, and the path it was loaded from, with these controls:

| Control | What it does |
|---|---|
| **Enable / Disable** | Turns the plugin on or off. The state is remembered between launches. |
| **Reload** | Hot-reloads a running plugin without restarting Cascade. Shown only while enabled. |
| **Configure** | Opens an inline settings form. Shown only for plugins that declare configurable options. |

A status badge next to each plugin shows whether it's currently **Enabled** or
**Disabled**.

## What a plugin can do

Once enabled, a plugin can:

- **Add commands** that appear in the [`/help`](commands.md) dialog under a
  **Plugin** category and run like any other slash command.
- **Decorate the UI**, for example by supplying colors or labels for nicknames.
- **React to events** such as connecting, messages, and joins, handled in the
  background.

!!! tip "Enable/disable is global"
    A plugin is on or off for the whole app. There's no per-channel or
    per-network toggle.

## Writing your own

Plugins talk to Cascade over JSON-RPC and can be written in any language. See the
[plugin system](../developers/plugin-system.md) developer reference for the
protocol, lifecycle, and examples.
