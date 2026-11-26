# Shell completion

Both `mirror-cli` and `admin-cli` support `completion bash|zsh|fish|powershell`. Generation works offline without login, configuration, or a database. Put both binaries on your `PATH` first (for a local build, `export PATH="$PWD/bin:$PATH"`).

For Bash, install the `bash-completion` package, version 2 or later. The scripts do not work with the macOS system Bash 3.2. On macOS use Zsh, or install a current Bash and `bash-completion@2` with Homebrew. Then run:

```bash
mkdir -p "$HOME/.local/share/bash-completion/completions"
mirror-cli completion bash > "$HOME/.local/share/bash-completion/completions/mirror-cli"
admin-cli completion bash > "$HOME/.local/share/bash-completion/completions/admin-cli"
source "$HOME/.local/share/bash-completion/completions/mirror-cli"
source "$HOME/.local/share/bash-completion/completions/admin-cli"
```

The files are loaded automatically in new sessions by `bash-completion`.

For Zsh, run:

```zsh
mkdir -p "$HOME/.zsh/completions"
mirror-cli completion zsh > "$HOME/.zsh/completions/_mirror-cli"
admin-cli completion zsh > "$HOME/.zsh/completions/_admin-cli"
```

Add these lines to `~/.zshrc`, placing the `fpath` line before any existing `compinit` call (only one `compinit` call is needed), then start a new shell:

```zsh
fpath=("$HOME/.zsh/completions" $fpath)
autoload -Uz compinit
compinit
```
