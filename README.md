# Prism

A macOS tool that sets up multiple iMessage relay services on a single Mac.

**What it does:** Prism creates macOS user accounts, installs an iMessage server + frpc tunnel for each user, and keeps everything running after reboot.

---

## Two Modes

Prism has two modes. You run them at different times.

### Host Mode (run as admin)

```bash
sudo ./prism
```

This mode:
1. Checks if your Mac has the right security settings (SIP disabled, etc.)
2. Installs Homebrew, Node.js, and frpc if missing
3. Creates macOS user accounts (e.g., `mymac-1`, `mymac-2`, `mymac-3`)
4. Downloads the iMessage server bundle to each user's home folder
5. Sets up auto-start so services restart after reboot

### User Mode (run as each created user)

```bash
./prism user
```

After switching to a created user account, this mode:
1. Triggers macOS permission dialogs (Messages access, etc.)
2. Starts the iMessage server and frpc tunnel
3. Gets an API key from the backend
4. Detects the user's phone number from Messages.app

---

## How It Works

### Step 1: Host Setup

When you run `sudo ./prism`, it does these things in order:

```
1. Preflight Checks
   - Runs: csrutil status
     → Must show "disabled"
   
   - Runs: nvram boot-args  
     → Must contain: amfi_get_out_of_my_way=1, amfi_allow_any_signature=1, etc.
   
   - Runs: defaults read /Library/Preferences/com.apple.security.libraryvalidation.plist DisableLibraryValidation
     → Must be 1 or true

2. Install Dependencies
   - If no Homebrew → install it
   - If no Node.js  → brew install node@18
   - If no frpc     → brew install frpc

3. Create Users (you enter how many, e.g., 3)
   - Creates: mymac-1, mymac-2, mymac-3
   - Each gets a random password (saved to output/secrets/users.csv)
   - Downloads server bundle to /Users/mymac-1/services/imsg/
   - Writes config.json and frpc.toml for each user

4. Set Up Auto-Start
   - Installs /Library/LaunchDaemons/com.prism.host-autoboot.plist
   - On reboot, this daemon starts all user services automatically
```

### Step 2: User Deploy

After logging into a created user (e.g., `mymac-1`), run `./prism user`:

```
1. Prewarm Permissions
   - Tries to read ~/Library/Messages/chat.db
   - Triggers macOS permission popups (grant them)

2. Deploy Services
   - Reads ~/services/imsg/config.json
   - Queries chat.db to find your phone number or email
   - Creates two LaunchAgents:
     - com.imsg.server.mymac-1.plist (the iMessage server)
     - com.imsg.frpc.mymac-1.plist (the tunnel)
   - Starts both with: launchctl bootstrap gui/501 <plist>
   - Waits for health check: http://localhost:10001/health

3. Get API Key
   - Sends POST to your backend's /keys/create endpoint
   - Shows you a one-time API key (copy it!)

4. (Optional) Rename Friendly Name
   - If auto-detection failed, manually set your phone number
```

---

## File Structure

```
Prism/
├── cmd/prism/main.go           # Entry point. Parses "user" or "host-autoboot" args.
│
├── internal/control/host/
│   └── init.go                 # Runs preflight → deps → provision in order
│
├── internal/ui/
│   ├── root/                   # Host mode TUI (the menu you see with sudo ./prism)
│   └── user/                   # User mode TUI (the menu you see with ./prism user)
│
├── internal/infra/
│   ├── macos/preflight.go      # Runs csrutil, nvram, defaults read
│   ├── deps/deps.go            # Installs Homebrew, Node, frpc
│   ├── host/
│   │   ├── users_provision.go  # Creates macOS users with sysadminctl
│   │   ├── per_user_files.go   # Downloads bundle, writes config.json/frpc.toml
│   │   ├── autoboot_daemon.go  # Installs the host LaunchDaemon
│   │   └── autoboot_run.go     # Called on reboot to start user services
│   ├── user/
│   │   ├── deploy.go           # Creates and starts LaunchAgents
│   │   ├── frpc_friendly_name.go  # Queries chat.db for phone/email
│   │   ├── key.go              # Requests API key from backend
│   │   └── permissions.go      # Triggers permission dialogs
│   ├── config/config.go        # Loads config/prism.json
│   └── state/state.go          # Loads/saves output/state.json
│
└── config/prism.json.example   # Example config file
```

---

## Quick Start

### 1. Create config file

```bash
cp config/prism.json.example config/prism.json
```

Edit `config/prism.json`:

```json
{
  "globals": {
    "machine_id": "mymac",
    "default_password": "",
    "frpc": {
      "server_addr": "your-frps-server.com",
      "server_port": 7000
    },
    "domain_suffix": "imsg.example.com",
    "service": {
      "archive_url": "gh://your-org/your-repo/bundle-macos-arm64.tar.gz",
      "start_port": 10001
    },
    "nexus": {
      "base_url": "https://your-backend.com"
    }
  }
}
```

### 2. Build

```bash
go build -o prism ./cmd/prism
```

### 3. Run host setup

```bash
sudo ./prism
```

Follow the TUI. It will:
- Check your Mac's security settings
- Install missing dependencies
- Ask how many users to create
- Create the users and download the server bundle

### 4. Switch to a created user and deploy

Log out, log in as `mymac-1` (password is in `output/secrets/users.csv`), then:

```bash
cd /Users/mymac-1/services/imsg
./prism user
```

Select "Deploy / start services" and follow prompts.

---

## Config Reference

### prism.json fields

| Field | What it does | Example |
|-------|--------------|---------|
| `machine_id` | Prefix for usernames | `"mymac"` → creates `mymac-1`, `mymac-2` |
| `default_password` | Password for new users. Empty = random. | `""` or `"MyPass123"` |
| `frpc.server_addr` | Your frps server address | `"frps.example.com"` |
| `frpc.server_port` | Your frps server port | `7000` |
| `domain_suffix` | Domain suffix for subdomains | `"imsg.example.com"` |
| `service.archive_url` | Where to download the server bundle | `"gh://org/repo/file.tar.gz"` |
| `service.start_port` | First user gets this port, next gets +1 | `10001` |
| `nexus.base_url` | Your backend API URL | `"https://api.example.com"` |

### Environment variables

| Variable | What it does |
|----------|--------------|
| `FRPC_TOKEN` | Auth token for frpc. Written to each user's frpc.toml. |
| `GITHUB_TOKEN` | Used when downloading from private GitHub repos. |
| `PRISM_CONFIG` | Override config file path (default: `config/prism.json`) |
| `PRISM_STATE` | Override state file path (default: `output/state.json`) |

---

## What Each File Does

### Preflight (`internal/infra/macos/preflight.go`)

Runs three commands to check macOS security settings:

```bash
# Check 1: SIP must be disabled
csrutil status
# → looks for "disabled" in output

# Check 2: boot-args must have AMFI flags
nvram boot-args
# → must contain: amfi_get_out_of_my_way=1, amfi_allow_any_signature=1, 
#   -arm64e_preview_abi, ipc_control_port_options=0

# Check 3: Library validation must be disabled
defaults read /Library/Preferences/com.apple.security.libraryvalidation.plist DisableLibraryValidation
# → must be "1" or "true"
```

### Dependencies (`internal/infra/deps/deps.go`)

Checks and installs three things:

```bash
# Check Homebrew
brew --version
# If missing → run Homebrew install script

# Check Node.js
node --version
# If missing → brew install node@18

# Check frpc
frpc --version  
# If missing → brew install frpc
```

### User Creation (`internal/infra/host/users_provision.go`)

Creates macOS users using the built-in `sysadminctl`:

```bash
sysadminctl -addUser mymac-1 -fullName mymac-1 -password "RandomPass123" -home /Users/mymac-1
```

### Service Deployment (`internal/infra/user/deploy.go`)

Creates two plist files in `~/Library/LaunchAgents/`:

- `com.imsg.server.mymac-1.plist` - runs the iMessage server
- `com.imsg.frpc.mymac-1.plist` - runs the frpc tunnel

Then starts them:

```bash
launchctl bootstrap gui/501 ~/Library/LaunchAgents/com.imsg.server.mymac-1.plist
launchctl bootstrap gui/501 ~/Library/LaunchAgents/com.imsg.frpc.mymac-1.plist
```

### Phone Number Detection (`internal/infra/user/frpc_friendly_name.go`)

Queries the Messages database to find the user's phone number or email:

```sql
SELECT
  CASE
    WHEN account LIKE 'P:%' THEN SUBSTR(account, 3)
    WHEN account LIKE 'E:%' THEN SUBSTR(account, 3)
    ELSE account
  END AS my_account
FROM message
WHERE is_from_me = 1
  AND account IS NOT NULL
  AND account != ''
ORDER BY
  CASE WHEN account LIKE 'P:%' THEN 1 ELSE 2 END
LIMIT 1;
```

- `P:+1234567890` → returns `+1234567890` (phone)
- `E:user@icloud.com` → returns `user@icloud.com` (email)
- Phone numbers are prioritized over emails.

---

## Building

### Local build

```bash
go build -o prism ./cmd/prism
```

### Smaller binary

```bash
go build -o prism -ldflags "-s -w" ./cmd/prism
```

### GitHub Actions release

Push a tag like `v1.0.0` and GitHub Actions will:
1. Build the binary
2. Package it as `prism-darwin-arm64.tar.gz`
3. Upload to GitHub Releases

---

## Troubleshooting

### "Preflight failed: SIP is enabled"

Boot into Recovery Mode (hold Cmd+R on restart), open Terminal, run:
```bash
csrutil disable
```
Then restart.

### "Permission denied" when running sudo ./prism

Make sure you're running as an admin user, not a standard user.

### Services not starting after user deploy

Check logs:
```bash
tail -100 ~/Library/Logs/imsg-server.log
tail -100 ~/Library/Logs/frpc.log
```

### Phone number not detected

Open Messages.app, send at least one iMessage, then try again. Or use "Rename friendly name" to set it manually.

---

## License

MIT
