---
name: sort
description: Sort new media out of the trawl inbox into the sorted library, splitting movies and shows and following the library's existing naming conventions. Builds the move plan, validates it, then runs it.
---

# /sort

The user wants to file new media that has landed in the inbox into the sorted
library. Request (may be empty): `$ARGUMENTS`

This mirrors the `/transfer` flow: you build JSON, validate it with the snitch
`validate` tool, then hand it to `trawl`. Never run `trawl` with JSON you have
not just validated — that holds for the list specs and the sort plan alike.

## Defaults

Unless the request overrides them:

- **inbox**: `~/Server/Library/Inbox` (the `box` host's `local_dir`)
- **library**: `~/Server/Library`

The inbox lives *inside* the library, so moves stay on one filesystem (fast,
atomic). When you list the library, treat `Inbox` like any other non-category
folder: **ignore it** — the only real categories are `Movies` and `Shows`.

## 1. See what arrived

List the inbox so you know what to sort:

```json
{"name":"box","side":"local","path":"~/Server/Library/Inbox","depth":1}
```

Validate it (`schema: "list_spec"`), then run `trawl --list '<json>'`. Read the
`tree`. Each top-level entry is one thing to file — usually a single `.mkv`, but
sometimes a release *directory* (move the whole directory as one unit). Ignore
non-media junk: `.txt`, `.nfo`, `.jpg`, sample files, `.part`/`.!ut` in-progress
downloads, and empty dirs.

## 2. Learn the library (this is the registry)

Do not keep a separate registry file — derive it live from the library tree so
it never drifts. List the library two levels deep:

```json
{"name":"box","side":"local","path":"~/Server/Library","depth":2}
```

Validate (`schema: "list_spec"`), run `trawl --list '<json>'`, and read it as:

- `Movies/<Title (Year)>/…` — every existing movie folder.
- `Shows/<Show.Name>/<Season.Folder>/…` — every show and its existing season
  folders.

This is what tells you whether a show already exists and what its season folders
are named, so a new episode lands beside its siblings instead of in a new
variant folder.

## 3. Decide where each inbox item goes

Classify by the filename, then build a destination **relative to the library**.

### Show episode

An item is an episode if its name contains a season/episode marker: `S01E02`,
`s01e02`, `1x02`, or a season pack like `S01` / `Season 1`.

Destination: `Shows/<Show.Name>/<Season.Folder>/<original name kept verbatim>`

- **Show.Name** is dotted, matching the library style: `Example.Show`,
  `Another.Show`. If the show already exists in the library, reuse that exact
  folder name — do not coin a new spelling.
- **Season.Folder** is `<Show.Name>.Sxx` (two-digit season). If a matching season
  folder already exists (even one carrying extra tags after the season number,
  e.g. `Example.Show.S04.extra.tags`), reuse it exactly. Only mint a new
  `<Show.Name>.Sxx` when that season has no folder yet.
- The **episode filename is never renamed** — it keeps its full original name.

### Movie

Anything without an episode marker is a movie.

Destination: `Movies/<Title (Year)>/<original name kept verbatim>`

- The **folder** is the clean human title with the year in parentheses:
  `Example Movie (2008)`, `Another Film (1999)`. Spaces, not dots; no
  quality tags in the folder name.
- The **file (or directory) inside keeps its original name** — do not
  rename the media itself. (See the library: `Example Movie (2008)/Example.Movie.2008.mkv`.)
- If the movie folder already exists, reuse it.

### When unsure

If you cannot confidently parse a title/year, or an item could plausibly go two
places, **leave it in the inbox** and list it back to the user rather than
guessing. Sorting the wrong file is worse than sorting none.

## 4. Build the sort plan

Assemble one JSON object. `src` is relative to the inbox, `dest` relative to the
library; both must be relative (no leading `/`, no `..`).

```json
{
  "inbox": "~/Server/Library/Inbox",
  "library": "~/Server/Library",
  "moves": [
    {"src": "Example.Show.S03E09.mkv",
     "dest": "Shows/Example.Show/Example.Show.S03/Example.Show.S03E09.mkv"},
    {"src": "Example.Movie.2008.mkv",
     "dest": "Movies/Example Movie (2008)/Example.Movie.2008.mkv"}
  ]
}
```

An empty `moves` list is valid — use it (or just tell the user) when nothing in
the inbox is sortable.

## 5. Validate the sort plan

Call the snitch `validate` tool with `schema: "sort_plan"` and your JSON as
`data`. If it returns `valid: false`, fix the named fields **in your JSON** and
validate again until valid. Do not run `trawl --sort` until it validates.

## 6. Confirm, then run

Show the user the planned moves (src → dest) and get a yes before touching files
— this writes to their library. On approval, run:

```
trawl --sort '<validated-json>'
```

`trawl` prints one `moved:` line per success and an `error:` line per failure,
then a `done: N moved, M failed` tally. It refuses to overwrite an existing
destination and continues past a failed move, so report the tally and surface any
`error:` lines verbatim. Anything that failed (or you deliberately skipped) stays
in the inbox for next time.

(`validate` is an early advisory check with clear field errors; `trawl` itself
re-validates the plan as the hard gate — advise, then enforce.)
