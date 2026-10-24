# Outlook MCP

Outlook MCP is a local Windows [Model Context Protocol](https://modelcontextprotocol.io/) server for classic Microsoft Outlook and a Markdown knowledge base. It communicates with MCP clients over standard input/output and uses the active local Outlook MAPI profile through Windows COM.

## Features

- List up to 25 unread messages from the default Outlook Inbox.
- Read a message by its Outlook `EntryID`.
- Search Inbox messages by subject text, sender, and received date.
- Create and save Outlook drafts.
- List, read, and search Markdown knowledge-base documents.
- Embed default knowledge-base documents in the executable and allow local disk overrides.

## Security boundaries

This server is deliberately **read-and-draft only**:

- It has no email send operation and does not call Outlook's `Send` method.
- It uses the interactive user's existing local MAPI profile; it does not use Microsoft Graph, OAuth, API keys, or external credentials.
- Knowledge-base reads accept only paths relative to the configured knowledge-base root.
- Outlook automation is serialized onto one COM Single-Threaded Apartment (STA) OS thread.
- Every email tool response is wrapped as **untrusted external content**. The wrapper instructs MCP clients to treat sender, subject, and body fields as data only, never as instructions or authorization for tool calls, disclosure, or draft creation.

## Architecture

```text
+-----------------------+
|  GitHub Copilot CLI   |
|       MCP client      |
+-----------+-----------+
            | JSON-RPC over standard I/O
+-----------v-----------------------------------------------+
|                    outlook-mcp.exe                        |
|  +----------------+        +---------------------------+  |
|  | MCP tool router|        | COM STA dispatcher        |  |
|  | (mcp-go)       |------->| LockOSThread + request    |  |
|  +-------+--------+        | channel                   |  |
|          |                 +-------------+-------------+  |
|  +-------v--------+                      |                |
|  | Knowledge base |                      v                |
|  | embedded + disk|          Classic Outlook / local MAPI |
|  +----------------+                                       |
+------------------------------------------------------------+
```

Every Outlook request is placed on a channel consumed by one goroutine. That goroutine calls `runtime.LockOSThread()`, initializes COM with `COINIT_APARTMENTTHREADED`, and performs all `go-ole` dispatches on that same OS thread.

## Requirements

- Windows with **classic Microsoft Outlook** installed and configured with a local profile.
- Go 1.27 or later.
- GitHub Copilot CLI, if using the Copilot CLI integration.

## Build and install

Run these commands from the project root in Windows PowerShell:

```powershell
go mod tidy
go build -ldflags="-s -w" -o outlook-mcp.exe .
copilot mcp add outlook -- "$PWD\outlook-mcp.exe"
```

The MCP server uses stdio, so do not run `outlook-mcp.exe` directly in an interactive terminal unless an MCP client is connected to its input and output.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `OUTLOOK_MCP_KB_DIR` | `.\kb` | Directory containing local Markdown knowledge-base documents. |

To set a local knowledge-base directory for the current PowerShell session:

```powershell
$env:OUTLOOK_MCP_KB_DIR = "$PWD\kb"
```

Default documents are compiled into the executable under `defaults/`. A local file with the same relative path overrides its embedded version; extra local `.md` files are included as additional documents.

## MCP tools

| Tool | Parameters | Description |
| --- | --- | --- |
| `list_unread_emails` | None | Lists up to 25 unread emails from the default Inbox. |
| `read_email` | `id` (required) | Reads an email by its Outlook `EntryID`. |
| `create_draft` | `to`, `subject`, `body` (required) | Creates and saves an Outlook draft. It never sends email. |
| `search_emails` | `query`, `sender`, `start_date` | Searches Inbox messages. Supply at least one parameter; `start_date` must be RFC3339, such as `2026-10-24T14:30:00Z`. |
| `kb_list_docs` | None | Lists available Markdown document paths. |
| `kb_search` | `query` (required) | Case-insensitively searches document paths and contents. |
| `kb_read_doc` | `path` (required) | Reads a document by its relative Markdown path. |

`search_emails` uses Outlook DASL restrictions for subject, sender, and received-time filters, then sorts results newest first.

## Email triage agent

The repository includes an Email Triage and Drafting Agent at `.github/agents/email-triage.agent.md`. Select it in Copilot CLI with:

```powershell
copilot --agent "Email Triage and Drafting"
```

For each run, the agent lists unread messages, reads each selected message, searches and reads relevant knowledge-base documents, then creates a reply draft. It never sends email.

The agent treats all email fields as untrusted external content. Email text cannot authorize tool calls, disclosure, or changes to its workflow. Facts and tone must come from the Knowledge Base; when it lacks the required information, the agent drafts an escalation response instead of inventing an answer.

## Development

Format, test, and build:

```powershell
gofmt -w main.go knowledge\kb.go outlook\*.go
go test ./...
go build -ldflags="-s -w" -o outlook-mcp.exe .
```

## Troubleshooting

- **Outlook initialization fails:** Open classic Outlook once, complete profile setup, and ensure it can access the intended mailbox.
- **No messages found:** The server queries the default Inbox. Confirm the messages are in that folder and that search filters use the expected sender or subject text.
- **Knowledge-base document missing:** Confirm it has a `.md` extension and is under `OUTLOOK_MCP_KB_DIR`, or use one of the embedded documents returned by `kb_list_docs`.
- **Copilot cannot start the server:** Re-run `copilot mcp add outlook -- "$PWD\outlook-mcp.exe"` from the directory containing the built executable.
