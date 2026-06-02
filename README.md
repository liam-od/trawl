# trawl

A fast, keyboard-driven, dual-pane terminal SFTP file manager. Connect to a host,
browse local and remote side by side, and copy files or whole directories with a
single keystroke. One static binary, secure defaults, no daemon.

## Install

`trawl` ships as a single static binary — no runtime, no dependencies.

### Linux (one-liner)

```sh
curl -fsSL https://raw.githubusercontent.com/liam-od/trawl/main/install.sh | sh
```

This downloads the latest release into `/usr/local/bin` (using `sudo` if needed). To install
somewhere on your own `PATH` without `sudo`:

```sh
curl -fsSL https://raw.githubusercontent.com/liam-od/trawl/main/install.sh | BIN_DIR="$HOME/.local/bin" sh
```

### Manual download

Grab the binary for your platform from the
[latest release](https://github.com/liam-od/trawl/releases/latest):

| Platform      | File                        |
|---------------|-----------------------------|
| Linux (amd64) | `trawl-linux-amd64`         |
| Windows (amd64) | `trawl-windows-amd64.exe` |

On Linux, mark it executable and move it onto your `PATH`:

```sh
chmod +x trawl-linux-amd64
sudo mv trawl-linux-amd64 /usr/local/bin/trawl
```

On Windows, drop `trawl-windows-amd64.exe` somewhere on your `PATH` (rename to `trawl.exe` if you
like) and run it from a terminal.

### From source

Requires Go 1.26 (the result is a static, CGO-free binary):

```sh
go install github.com/liam-od/trawl/cmd/trawl@latest    # → $GOBIN
```

or clone and build:

```sh
git clone https://github.com/liam-od/trawl
cd trawl
make build            # → bin/trawl
make build-all        # → bin/trawl-linux-amd64, bin/trawl-windows-amd64.exe
```

## Usage

```sh
trawl [flags] user@host[:port][:/remote/path]
```

Examples:

```sh
trawl me@example.com                 # start in your remote home directory
trawl me@example.com:2222:/var/www   # custom port and starting path
```

On the first connection to a host you'll be shown its key fingerprint and asked
whether to add it to `~/.ssh/known_hosts`. After that, a changed host key is
refused automatically.

### One-time setup (optional)

Save your defaults so you can just type `trawl host`:

```sh
trawl --setup        # prompts for default user, key path, port, password fallback
```

Settings resolve as: command-line flag > config file (`~/.config/trawl/config.json`) > built-in default.

### Authentication

The SSH agent is tried first, then a key file (`--key`, or `key_path` from setup),
then an interactive password prompt. To use a specific key:

```sh
ssh-add ~/.ssh/id_ed25519      # via the agent (recommended), or…
trawl --key ~/.ssh/id_ed25519 me@host
```

### Keys

| Key | Action |
|-----|--------|
| `Tab` | switch active panel |
| `↑` / `↓` (or `k` / `j`) | move the cursor |
| `Enter` | open directory |
| `Backspace` (or `h`) | parent directory |
| `F5` / `c` | copy the selected file or directory to the other panel |
| `F7` | show / hide the transfer queue |
| `r` | refresh the active panel |
| `q` (or `F10`, `Ctrl+C`) | quit |

Copies run one at a time through the transfer queue, which shows progress and the
current transfer rate. Press `F5`/`c` again while one is running to queue more.

### Flags

```
--port N          SSH port (overrides :port in the target; default 22)
--user NAME       override the user in the target
--key PATH        private key file (otherwise the SSH agent is used)
--password        allow password authentication as a fallback (default true)
--no-password     disable the password fallback
--known-hosts P   known_hosts file (default ~/.ssh/known_hosts)
--config PATH     config file (default ~/.config/trawl/config.json)
--setup           run the interactive setup wizard and exit
--version         print version and exit
--help            show this help and exit
```
