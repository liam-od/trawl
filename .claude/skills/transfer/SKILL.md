---
name: transfer
description: Download or upload a file with trawl. Generates the transfer spec, validates it, then runs it.
---

# /transfer

The user wants to move a file with trawl. Request: `$ARGUMENTS`
You have access to the `trawl` binary on the current path.

Every `trawl` invocation is preceded by a snitch `validate` call on the same JSON.
That rule holds for **both** the list spec and the transfer spec — never run
`trawl` with JSON you have not just validated. The steps below spell out, in
order, what to build, what to validate, and what to run.

## 1. List first, so you know what's there

Skip this whole section only if the request already names an exact `object`.
Otherwise look before you leap.

### 1a. Build the list spec

Build a list spec for the side you'll read from — `remote` for a download,
`local` for an upload:

```json
{"name":"box","side":"remote","path":"/srv/media","depth":1}
```

- `name` — the saved host alias (same one the transfer will use).
- `side` — `remote` (download source) or `local` (upload source).
- `path` — include only to override the host's configured base dir.
- `depth` — **default to `1`**: shows top-level items only. Increase to `2` only
  if the target isn't visible at depth 1 and you need to see inside a directory;
  omit entirely only when the user explicitly wants the full tree.

### 1b. Validate the list spec

Call the snitch `validate` tool with `schema: "list_spec"` and your JSON as
`data`. If it returns `valid: false`, fix the named fields **in your JSON** and
validate again until valid. Do not run `trawl --list` until it validates.

### 1c. Run the list

Run `trawl --list '<validated-json>'` and read the returned `tree` to pick the
real `object` (the path relative to the reported `path`). This is how "download
the latest episode" becomes a concrete filename.

If the target isn't found, re-list with `depth:2` to look inside directories, or
narrow to a specific `path` and re-list rather than raising depth across the whole tree.

## 2. Build the transfer spec

Build the JSON object for `trawl --transfer`:

- `name` — the saved host alias. Ask if the request doesn't name one.
- `type` — `remote_to_local` (download) or `local_to_remote` (upload).
- `object` — the file or directory within the host's base dir; omit to move the
  whole dir. **Relative only**: no leading slash, no `..` segment.
- `remote_path` / `local_path` — include only to override the host's configured
  base directories.

## 3. Validate the transfer spec

Call the snitch `validate` tool with `schema: "transfer_spec"` and your JSON as
`data`. If it returns `valid: false`, fix the named fields **in your JSON** and
validate again until valid. Do not run `trawl --transfer` until it validates.

## 4. Run the transfer

Run `trawl --transfer '<validated-json>'` with the Bash `run_in_background: true`
flag. Transfers can run for many minutes — longer than the foreground Bash
timeout — so they must be backgrounded. The harness notifies you when the
command exits; do not poll on a timer. (Read the task's output file only if the
user asks for mid-transfer progress.)

When the command exits, report trawl's `done:` line on success. On a non-zero
exit, surface trawl's `error:` line verbatim.

(`validate` is an early advisory check with clear field errors; `trawl` itself
re-validates the spec as the hard gate — advise, then enforce.)
