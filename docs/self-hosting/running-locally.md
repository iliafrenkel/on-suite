# Running ON Suite on your own computer

> Want it on a server for your whole household instead? See
> [Deploying ON Suite](deploying.md).

ON Suite doesn't need a server. You can run it on your own Mac, Linux or
Windows computer like any other app: double-click an icon, and ON Suite opens
in your web browser. Only you can reach it, because it listens on your
computer alone.

This guide sets up that icon. You do it once, in four steps:

1. [Download ON Suite](#1-download-on-suite)
2. [Create your account](#2-create-your-account)
3. [Make a launcher](#3-make-a-launcher)
4. [Start it](#4-start-it)

It uses the terminal a few times (Terminal on a Mac, PowerShell on Windows).
Copy each command exactly; you don't need to know what it does.

## Where things go

| | macOS | Linux | Windows |
|---|---|---|---|
| The program | `~/Applications/ON Suite` | `~/.local/opt/onsuite` | `%LOCALAPPDATA%\Programs\ON Suite` |
| Your data | `~/Library/Application Support/ON Suite` | `~/.local/share/onsuite` | `%LOCALAPPDATA%\ON Suite` |

The program folder holds the `onsuite` program and your launcher. The data
folder holds everything you create, plus automatic daily backups. Keep them
apart: when you update ON Suite you replace the program, never the data.

## 1. Download ON Suite

Go to the [releases page](https://github.com/iliafrenkel/on-suite/releases)
and pick the file for your computer from the newest release:

| Computer | File |
|---|---|
| Mac with Apple silicon (M1 or newer) | `on-suite_<version>_darwin_arm64.tar.gz` |
| Linux on a PC | `on-suite_<version>_linux_amd64.tar.gz` |
| Linux on ARM (e.g. a 64-bit Raspberry Pi) | `on-suite_<version>_linux_arm64.tar.gz` |
| Windows | `on-suite_<version>_windows_amd64.zip` |

Intel Macs and 32-bit systems aren't supported.

### macOS

Downloading with the terminal rather than the browser saves a security
warning later. Replace `2.0.0` with the newest version:

```bash
mkdir -p ~/Applications/"ON Suite" && cd ~/Applications/"ON Suite"
curl -LO https://github.com/iliafrenkel/on-suite/releases/download/v2.0.0/on-suite_2.0.0_darwin_arm64.tar.gz
tar -xzf on-suite_2.0.0_darwin_arm64.tar.gz && rm on-suite_2.0.0_darwin_arm64.tar.gz
```

If you downloaded it with the browser instead, macOS will refuse to open it
("cannot be opened because the developer cannot be verified"), because ON
Suite isn't registered with Apple. Clear that once from the ON Suite folder:

```bash
xattr -d com.apple.quarantine onsuite
```

### Linux

Replace `2.0.0` with the newest version, and `amd64` with `arm64` on ARM:

```bash
mkdir -p ~/.local/opt/onsuite && cd ~/.local/opt/onsuite
curl -LO https://github.com/iliafrenkel/on-suite/releases/download/v2.0.0/on-suite_2.0.0_linux_amd64.tar.gz
tar -xzf on-suite_2.0.0_linux_amd64.tar.gz && rm on-suite_2.0.0_linux_amd64.tar.gz
```

### Windows

Download the `.zip` file with your browser, then unpack it from PowerShell.
Replace `2.0.0` with the version you downloaded:

```powershell
Expand-Archive "$HOME\Downloads\on-suite_2.0.0_windows_amd64.zip" -DestinationPath "$env:LOCALAPPDATA\Programs\ON Suite" -Force
```

## 2. Create your account

Do this once. The command asks for a password twice and doesn't show it as
you type. Use at least 12 characters. Replace `alex` with the username you
want: 3 to 32 letters or digits, and it may contain dots, dashes and
underscores in the middle.

**macOS:**

```bash
cd ~/Applications/"ON Suite"
./onsuite user add alex --admin --data-dir ~/Library/Application\ Support/"ON Suite"
```

**Linux:**

```bash
cd ~/.local/opt/onsuite
./onsuite user add alex --admin --data-dir ~/.local/share/onsuite
```

**Windows** (in PowerShell):

```powershell
cd "$env:LOCALAPPDATA\Programs\ON Suite"
.\onsuite.exe user add alex --admin --data-dir "$env:LOCALAPPDATA\ON Suite"
```

You're the admin of your own ON Suite, so you can add accounts for other
people on this computer later, from **Admin → Manage users**.

## 3. Make a launcher

The launcher is a small file you double-click. It starts ON Suite, waits
until it's ready, and opens it in your browser. It keeps a window open while
ON Suite runs; closing that window stops ON Suite.

### macOS

In the ON Suite folder, create the launcher and allow it to run:

```bash
cd ~/Applications/"ON Suite"
cat > "ON Suite.command" <<'EOF'
#!/bin/bash
# ON Suite launcher: double-click to start, close this window to stop.
cd "$(dirname "$0")"
DATA="$HOME/Library/Application Support/ON Suite"
mkdir -p "$DATA"
(until curl -fs http://127.0.0.1:8080/healthz >/dev/null; do sleep 0.5; done
 open http://localhost:8080/) &
exec ./onsuite serve --addr 127.0.0.1:8080 --data-dir "$DATA"
EOF
chmod +x "ON Suite.command"
```

To put it in the Dock, drag **ON Suite.command** from Finder onto the right
side of the Dock, next to the Downloads folder.

### Linux

Create the launcher script and a menu entry for it:

```bash
cd ~/.local/opt/onsuite
cat > onsuite-launch <<'EOF'
#!/bin/sh
# ON Suite launcher: starts ON Suite and opens it in the browser.
cd "$(dirname "$0")"
DATA="$HOME/.local/share/onsuite"
mkdir -p "$DATA"
(until curl -fs http://127.0.0.1:8080/healthz >/dev/null; do sleep 0.5; done
 xdg-open http://localhost:8080/) &
exec ./onsuite serve --addr 127.0.0.1:8080 --data-dir "$DATA"
EOF
chmod +x onsuite-launch

mkdir -p ~/.local/share/applications
cat > ~/.local/share/applications/onsuite.desktop <<EOF
[Desktop Entry]
Type=Application
Name=ON Suite
Comment=Notes, snippets, feeds, saved articles and flash cards
Exec=$HOME/.local/opt/onsuite/onsuite-launch
Terminal=true
Categories=Office;
EOF
```

**ON Suite** now appears in your applications menu. If your system doesn't
have `curl`, install it first (for example `sudo apt install curl`).

### Windows

Open Notepad, paste the following, and save it as `ON Suite.bat` in
`%LOCALAPPDATA%\Programs\ON Suite` (in the Save dialog, set **Save as type**
to **All files** so Notepad doesn't add `.txt`):

```bat
@echo off
rem ON Suite launcher: double-click to start, close this window to stop.
cd /d "%~dp0"
set "DATA=%LOCALAPPDATA%\ON Suite"
if not exist "%DATA%" mkdir "%DATA%"
start "" /b onsuite.exe serve --addr 127.0.0.1:8080 --data-dir "%DATA%"
:wait
timeout /t 1 /nobreak >nul
curl.exe -fs http://127.0.0.1:8080/healthz >nul 2>&1 || goto wait
start "" http://localhost:8080/
echo ON Suite is running. Close this window to stop it.
pause >nul
```

To add it to the Start menu, right-click **ON Suite.bat**, choose **Show more
options → Send to → Desktop (create shortcut)**, then drag the shortcut into
`%APPDATA%\Microsoft\Windows\Start Menu\Programs`.

The first time you run it, Windows may show **Windows protected your PC**,
because ON Suite isn't registered with Microsoft. Click **More info → Run
anyway**.

## 4. Start it

Double-click the launcher. A window opens, and a moment later ON Suite opens
in your browser at <http://localhost:8080/>. Sign in with the account you
created. From here on, the [user guides](../user/index.md) explain every app.

To stop ON Suite, close the launcher's window. Your data stays in the data
folder until you start it again.

### If it doesn't open

- **"address already in use"** in the window: something else on your
  computer uses port 8080, or ON Suite is already running in another window.
  Close the other window, or change `8080` to another number, such as `8090`,
  everywhere in the launcher.
- **The browser says it can't connect:** check the launcher's window for an
  error message. It stays open while ON Suite runs, so if it closed straight
  away, open a terminal in the program folder and run the launcher from
  there to see what it says.

## Using it from your phone

The launcher only lets this computer in. To use ON Suite from other devices
on your home network, change `--addr 127.0.0.1:8080` to `--addr :8080` in
the launcher, then open `http://<your computer's address>:8080/` on the other
device. The address is your computer's name followed by `.local` on most
home networks (for example `http://my-laptop.local:8080/`), or its IP
address, which you'll find in your network settings. Only do this on a network you trust: it's plain HTTP, so for
anything wider, set it up on a server with HTTPS instead (see
[Deploying ON Suite](deploying.md)).

## Updating

Your data survives updates; you only replace the program.

1. Close the launcher's window to stop ON Suite.
2. **Take a backup with the version you have now**, before replacing it. A
   new version updates the database the first time it runs, and there's no
   way back except a backup. From the program folder:

   - macOS: `./onsuite backup --data-dir ~/Library/Application\ Support/"ON Suite"`
   - Linux: `./onsuite backup --data-dir ~/.local/share/onsuite`
   - Windows: `.\onsuite.exe backup --data-dir "$env:LOCALAPPDATA\ON Suite"`

3. Download the new release and unpack it into the program folder, as in
   [step 1](#1-download-on-suite), replacing the old files. Your launcher
   stays; you don't need to make it again.
4. Double-click the launcher.

The footer at the bottom of every page shows which version you're running.

## Backups

ON Suite saves a snapshot of your database after it has been running for a
full day, and every day after that, keeping the last seven in the `backups`
folder inside your data folder. If you only open it now and then, that may
never happen, so take a backup yourself from time to time with the same
command as in [Updating](#updating) (step 2). Each run adds one snapshot to
the `backups` folder.

To keep a copy somewhere safer, copy the whole data folder to another disk or
cloud storage while ON Suite is stopped.

## Uninstalling

Delete the program folder. To delete everything you created too, delete the
data folder. On Linux, also delete
`~/.local/share/applications/onsuite.desktop`.
