#!/usr/bin/env bash
# Installs the Fleet Order Notifier extension into a Steam copy of
# X4: Foundations (Linux / Steam Proton).
#
# It finds the game, asks you to confirm the folder, checks that the djfhe_http
# dependency is installed, copies fleet_notifier/ into extensions/ and can set
# the shared token in the Lua file.
#
# Usage:
#   ./scripts/install.sh                       interactive
#   ./scripts/install.sh --game-path <dir>     use a specific game folder
#   ./scripts/install.sh --token <token>       pre-set the shared token
#   ./scripts/install.sh --yes                 accept every confirmation
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$root/fleet_notifier"

game_path=""
token=""
install_dir=""
assume_yes=0

usage() {
    cat <<'EOF'
Install the Fleet Order Notifier extension into X4: Foundations and copy the
server files to a folder of their own.

Options:
  --game-path <dir>   X4 install folder (the one containing X4.exe)
  --install-dir <dir> where the server lives (default: ~/.local/share/x4-notifier)
  --token <token>     shared token, written into ui/notifier.lua and .env
                      (allowed characters: A-Z a-z 0-9 . _ -)
  -y, --yes           accept every confirmation
  -h, --help          show this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --game-path) game_path="${2:-}"; shift 2 ;;
        --game-path=*) game_path="${1#*=}"; shift ;;
        --install-dir) install_dir="${2:-}"; shift 2 ;;
        --install-dir=*) install_dir="${1#*=}"; shift ;;
        --token) token="${2:-}"; shift 2 ;;
        --token=*) token="${1#*=}"; shift ;;
        -y|--yes) assume_yes=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    esac
done

say() { printf '%s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die() { warn "$*"; exit 1; }

confirm() {
    local prompt="$1"
    if [ "$assume_yes" -eq 1 ] || [ ! -t 0 ]; then
        return 0
    fi
    local answer
    read -r -p "$prompt [Y/n] " answer || return 0
    case "${answer:-y}" in
        [nN]*) return 1 ;;
        *) return 0 ;;
    esac
}

steam_roots() {
    [ -n "${HOME:-}" ] || return 0
    local candidate
    for candidate in \
        "$HOME/.steam/steam" \
        "$HOME/.local/share/Steam" \
        "$HOME/.var/app/com.valvesoftware.Steam/.local/share/Steam" \
        "$HOME/snap/steam/common/.local/share/Steam"; do
        if [ -d "$candidate" ]; then
            printf '%s\n' "$candidate"
        fi
    done
}

# Reads the extra library folders from Steam's libraryfolders.vdf.
# New format: "path" "/games/Steam"; old format: "1" "/games/Steam".
libraries_from_vdf() {
    local vdf="$1/steamapps/libraryfolders.vdf"
    [ -f "$vdf" ] || return 0
    sed -n 's/^[[:space:]]*"\(path\|[0-9][0-9]*\)"[[:space:]]*"\([^"]*\)".*/\2/p' "$vdf"
}

find_game() {
    if [ -n "$game_path" ]; then
        printf '%s\n' "$game_path"
        return 0
    fi

    local steam_root library candidate
    local libraries=()
    while IFS= read -r steam_root; do
        libraries=("$steam_root")
        while IFS= read -r library; do
            [ -n "$library" ] && libraries+=("$library")
        done < <(libraries_from_vdf "$steam_root")

        for library in "${libraries[@]}"; do
            candidate="$library/steamapps/common/X4 Foundations"
            if [ -f "$candidate/X4.exe" ]; then
                printf '%s\n' "$candidate"
                return 0
            fi
        done
    done < <(steam_roots)

    return 1
}

ask_game_path() {
    if [ ! -t 0 ]; then
        return 1
    fi
    local answer
    read -r -p "Path to the X4 Foundations folder (the one containing X4.exe): " answer || return 1
    [ -n "$answer" ] || return 1
    game_path="${answer%\"}"
    game_path="${game_path#\"}"
}

# --- 1. locate the game -----------------------------------------------------

say "X4 Fleet Order Notifier installer"
say ""

if [ -n "$game_path" ] && [ ! -f "$game_path/X4.exe" ]; then
    die "'$game_path' does not look like an X4 install (no X4.exe there)."
fi

game=""
detected="$(find_game || true)"
if [ -n "$detected" ] && [ -f "$detected/X4.exe" ]; then
    say "X4: Foundations found at:"
    say "  $detected"
    if confirm "Install into this folder?"; then
        game="$detected"
    fi
elif [ -n "$detected" ]; then
    warn "'$detected' does not look like an X4 install (no X4.exe there)."
else
    warn "Could not find X4: Foundations in a Steam library."
    warn "Only the Steam version is detected automatically."
fi

while [ -z "$game" ]; do
    if [ ! -t 0 ]; then
        die "No valid game folder; nothing was installed. Pass --game-path <dir>."
    fi
    if ! ask_game_path; then
        die "No game folder confirmed; nothing was installed."
    fi
    if [ -f "$game_path/X4.exe" ]; then
        game="$game_path"
    else
        warn "'$game_path' does not look like an X4 install (no X4.exe there)."
        game_path=""
    fi
done

say ""
say "Game folder: $game"

# --- 2. check the dependency ------------------------------------------------

extensions_dir="$game/extensions"
djfhe_dir="$extensions_dir/djfhe_http"

if [ ! -f "$djfhe_dir/content.xml" ]; then
    warn "djfhe_http is not installed in:"
    warn "  $djfhe_dir"
    warn ""
    warn "Download it from https://github.com/djfhe/x4_http and unpack it so that"
    warn "content.xml ends up in that folder (rename the extracted folder to djfhe_http)."
    die "Cannot continue without the djfhe_http extension."
fi

say "djfhe_http: found"

# --- 3. copy the mod --------------------------------------------------------

if [ ! -d "$source_dir" ]; then
    die "Cannot find $source_dir. Run this script from a copy of the repository."
fi

destination="$extensions_dir/fleet_notifier"
if [ -e "$destination" ]; then
    say ""
    say "An earlier installation exists at:"
    say "  $destination"
    if ! confirm "Replace it?"; then
        die "Nothing was installed."
    fi
    rm -rf "$destination"
fi

mkdir -p "$extensions_dir"
cp -R "$source_dir" "$destination"
say "fleet_notifier: copied to $destination"

# --- 4. optional shared token ----------------------------------------------

if [ -z "$token" ] && [ "$assume_yes" -eq 0 ] && [ -t 0 ]; then
    say ""
    read -r -p "Shared token for the local server (must match X4_TOKEN in .env, empty for none): " token || true
fi

if [ -n "$token" ]; then
    if [[ ! "$token" =~ ^[A-Za-z0-9._-]+$ ]]; then
        warn "The token has characters the script will not write (allowed: A-Z a-z 0-9 . _ -)."
        warn "Set it by hand in $destination/ui/notifier.lua"
    else
        sed -i "s|^local TOKEN = .*|local TOKEN = \"$token\"|" "$destination/ui/notifier.lua"
        say "Token: written to ui/notifier.lua"
    fi
fi

# --- 5. deploy the server folder -------------------------------------------

say ""
install_dir="${install_dir:-${XDG_DATA_HOME:-$HOME/.local/share}/x4-notifier}"
say "Server folder:"
say "  $install_dir"
deploy_server=1
if ! confirm "Copy the server files there?"; then
    deploy_server=0
    warn "Server files not copied; only the extension was installed."
fi

if [ "$deploy_server" -eq 1 ]; then
    mkdir -p "$install_dir"

    if [ -f "$root/bin/x4-notifier-linux-amd64" ]; then
        cp -f "$root/bin/x4-notifier-linux-amd64" "$install_dir/"
        chmod +x "$install_dir/x4-notifier-linux-amd64"
        say "Server binary: copied"
    else
        warn "$root/bin/x4-notifier-linux-amd64 is missing; copy the binary yourself."
    fi

    env_file="$install_dir/.env"
    if [ -f "$root/bin/.env.example" ] && [ ! -f "$env_file" ]; then
        if [ -n "$token" ]; then
            sed "s|^X4_TOKEN=.*|X4_TOKEN=$token|" "$root/bin/.env.example" > "$env_file"
        else
            cp "$root/bin/.env.example" "$env_file"
        fi
        say "Configuration: created $env_file"
    elif [ -f "$env_file" ]; then
        say "Configuration: kept the existing $env_file"
        if [ -n "$token" ]; then
            warn "make sure X4_TOKEN in $env_file matches the token you entered."
        fi
    else
        warn "bin/.env.example is missing; create $env_file yourself."
    fi
fi

# --- 6. what is left --------------------------------------------------------

say ""
say "Done. Installed to:"
say "  $destination"
if [ "$deploy_server" -eq 1 ]; then
    say "Server folder:"
    say "  $install_dir"
fi
say ""
say "Next steps:"
say "  1. In X4: Settings > Extensions, enable 'djfhe http' and 'Fleet Order Notifier'."
say "  2. In X4: Settings > Game Settings, turn Protected UI Mode OFF."
if [ "$deploy_server" -eq 1 ]; then
    say "  3. Edit $install_dir/.env with your notification app values"
    say "     (see docs/notification-apps-guide.md)."
    say "  4. Start the server and keep it running while you play:"
    say "     cd '$install_dir' && ./x4-notifier-linux-amd64"
else
    say "  3. Copy bin/.env.example to bin/.env, fill in the notification app values"
    say "     (see docs/notification-apps-guide.md) and start the server from there."
fi
