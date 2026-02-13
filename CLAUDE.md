# CLAUDE.md - AIPilot CLI

Ce fichier fournit le contexte pour Claude Code travaillant sur le projet CLI.

## Vue d'ensemble

Le CLI AIPilot est un daemon Go qui spawn un PTY (pseudo-terminal) avec un agent IA (claude, gemini, codex) et permet le controle depuis un mobile via un relay WebSocket. Le CLI agit comme bridge entre le terminal local et l'application mobile.

```
┌─────────────┐     WebSocket     ┌─────────────┐     WebSocket     ┌─────────────┐
│   Mobile    │◄─────────────────►│   Relay     │◄─────────────────►│  CLI (PTY)  │
│   (xterm)   │                   │ Cloudflare  │                   │   + Agent   │
└─────────────┘                   └─────────────┘                   └─────────────┘
```

---

## Commandes de build

```bash
cd aipilot-cli

# Build avec version
make build

# Run en dev
make run

# Build simple
go build .

# Voir la version
make version

# Bump version
make patch   # 0.0.X
make minor   # 0.X.0
make major   # X.0.0
```

---

## Structure des fichiers

| Fichier | Role |
|---------|------|
| `main.go` | Entry point, parsing flags, demarrage PTY, boucle I/O |
| `types.go` | Struct `Daemon`, `Message`, constantes ANSI |
| `constants.go` | Timeouts, buffer sizes, permissions |
| `agents.go` | Detection agents IA (claude, gemini, codex), selection |
| `encryption.go` | AES-256-GCM pour chiffrement donnees session |
| `crypto.go` | X25519 ECDH, NaCl box pour chiffrement tokens |
| `websocket.go` | Connexion relay, gestion messages WebSocket |
| `terminal.go` | PTY resize, switch client (PC/mobile), I/O PTY |
| `pairing.go` | Configuration PC, gestion mobiles appaires, QR data |
| `pairing_handlers.go` | Affichage QR, unpair, status PC |
| `relay_api.go` | Client REST pour API relay |
| `session.go` | Persistance sessions locales |
| `ssh.go` | Detection SSH, installation cles authorized_keys |
| `commands_session.go` | Commande /qr (affichage QR pairing) |
| `commands_pairing.go` | Affichage QR pairing depuis daemon |
| `commands_info.go` | Envoi info CLI au mobile |
| `commands_terminal.go` | Traitement messages controle (resize, file-upload) |
| `commands_upload.go` | Upload fichiers (simple et chunked) |
| `utils.go` | Utilitaires (openFile cross-platform) |
| `signal_unix.go` | Gestion signaux SIGWINCH (Unix) |
| `signal_windows.go` | Gestion signaux (Windows) |

---

## Messages de controle

Les messages de controle passent par le canal data avec prefix `\x00CTRL:`.

### Envoyes (CLI -> Mobile)

| Message | Format | Description |
|---------|--------|-------------|
| `mode` | `mode:pc\|mobile` | Client actif (dimensions PTY) |
| `cli-info` | `cli-info:{json}` | Info systeme (os, hostname, user, ssh, ips) |
| `file-upload-ack` | `file-upload-ack:uploadId:chunkIndex` | Confirmation chunk recu |
| `file-upload-result` | `file-upload-result:success\|error:path\|msg` | Resultat upload |
| `ssh-setup-result` | `ssh-setup-result:success\|error:message` | Resultat installation cle SSH |

### Recus (Mobile -> CLI)

| Message | Format | Description |
|---------|--------|-------------|
| `resize` | `resize:cols,rows` | Changement taille terminal mobile |
| `info-request` | `info-request` | Demande info CLI |
| `ssh-setup-key` | `ssh-setup-key:username:mobileId:keyBase64` | Installation cle SSH |
| `file-upload` | `file-upload:filename:dataBase64` | Upload fichier simple |
| `file-upload-start` | `file-upload-start:uploadId:filename:totalChunks:totalSize` | Debut upload chunked |
| `file-upload-chunk` | `file-upload-chunk:uploadId:chunkIndex:dataBase64` | Chunk upload |
| `file-upload-cancel` | `file-upload-cancel:uploadId` | Annule upload chunked |

---

## API REST (Relay)

Le CLI appelle ces endpoints sur le relay:

### Pairing

| Endpoint | Methode | Description |
|----------|---------|-------------|
| `/api/pairing/init` | POST | Demarre appairage, retourne token |
| `/api/pairing/status` | GET | Verifie completion appairage |
| `/api/pairing/mobiles` | GET | Liste mobiles appaires |
| `/api/pairing/mobiles/:id` | DELETE | Supprime mobile appaire |

### Sessions

| Endpoint | Methode | Description |
|----------|---------|-------------|
| `/api/sessions` | POST | Cree session avec tokens chiffres |
| `/api/sessions` | DELETE | Purge toutes les sessions (PC) |
| `/api/sessions/:id` | DELETE | Supprime une session |
| `/api/sessions/:id/tokens` | POST | Ajoute token pour nouveau mobile |

### WebSocket

| Endpoint | Description |
|----------|-------------|
| `/ws/{sessionId}?role=bridge` | Connexion CLI au relay |

---

## Commandes utilisateur

Commandes tapees dans le terminal:

| Commande | Action |
|----------|--------|
| `/qr` | Affiche QR pairing (alternate screen, ESC/Ctrl+C pour quitter) |

Note: Le cleanup de session est automatique quand l'agent se termine (`/exit`, Ctrl+C, etc.)

### Flags CLI

```bash
aipilot-cli [options]

--agent <name>      # Agent a executer (claude, gemini, codex)
--agent ?           # Force re-selection de l'agent
--workdir <path>    # Repertoire de travail
--agents            # Liste agents disponibles
--sessions          # Liste sessions sauvegardees
--unpair <id>       # Supprime un mobile appaire
--status            # Affiche statut PC et mobiles
--version           # Affiche version
```

---

## Configuration

### Fichiers de config

```
~/.config/aipilot/
├── config.json        # ID PC, cles X25519, mobiles appaires
└── directories.json   # Agent par defaut par repertoire

~/.aipilot/sessions/   # Sessions sauvegardees (hash du workdir)
```

### Structure config.json

```json
{
  "pc_id": "uuid",
  "pc_name": "hostname",
  "private_key": "hex-encoded-x25519-private",
  "public_key": "hex-encoded-x25519-public",
  "paired_mobiles": [
    {
      "id": "mobile-uuid",
      "name": "iPhone de Francois",
      "public_key": "hex-encoded",
      "paired_at": "RFC3339"
    }
  ],
  "created_at": "RFC3339"
}
```

---

## Securite

### Chiffrement sessions (AES-256-GCM)

```
Session token (32 chars) -> SHA256 -> 256-bit AES key
Message: base64(nonce[12] || ciphertext || tag[16])
```

Implemente dans `encryption.go`:
- `initEncryption()`: derive cle depuis token
- `encrypt()`: chiffre avec nonce aleatoire
- `decrypt()`: dechiffre et verifie tag

### Chiffrement tokens (X25519 + NaCl Box)

Pour transmettre le token session au mobile de maniere securisee:

```
CLI: Box.Seal(token, nonce, mobile_public, cli_private)
Mobile: Box.Open(encrypted, nonce, cli_public, mobile_private)
Format: hex(nonce[24] || ciphertext)
```

Implemente dans `crypto.go`:
- `GenerateX25519KeyPair()`: genere paire de cles
- `EncryptForMobile()`: chiffre pour un mobile specifique
- `GetPrivateKeyFromHex()`: parse cle privee

---

## Code mort connu

Les elements suivants sont inutilises et peuvent etre supprimes:

### Constantes (types.go)

```go
const (
    clearLine  = "\033[K"     // Non utilise
    moveUp     = "\033[1A"    // Non utilise
    moveToCol0 = "\r"         // Non utilise
)
```

### Fonctions (types.go)

```go
func (d *Daemon) isRelayConnected() bool  // Non utilise
```

### Fonctions (terminal.go)

```go
func (d *Daemon) forceResize()                    // Non utilise
func (d *Daemon) writeToPTY(data []byte) (int, error)  // Remplace par sendToPTY
```

### Fonctions (relay_api.go)

```go
func (c *RelayClient) ListPairedMobiles() ([]PairedMobile, error)  // Non utilise
```

### Types (relay_api.go)

```go
type PairingCompleteRequest struct { ... }  // Utilise cote mobile, pas CLI
```

---

## Patterns importants

### PTY I/O

Le daemon maintient une boucle de lecture PTY:

```go
// Lecture PTY -> stdout + mobile
go func() {
    buf := make([]byte, BufferSize)
    for {
        n, err := daemon.readFromPTY(buf)
        os.Stdout.Write(buf[:n])
        daemon.sendToMobile(buf[:n])
    }
}()

// Lecture stdin -> PTY (avec detection commandes //)
go func() {
    for {
        b := make([]byte, 1)
        os.Stdin.Read(b)
        // Detection /qr command
        daemon.sendToPTY(b)
    }
}()
```

### Switch client (PC/Mobile)

Le PTY adapte sa taille selon le client actif:

```go
func (d *Daemon) switchToClient(client string) {
    if client == "mobile" {
        cols, rows = d.mobileCols, d.mobileRows
    } else {
        cols, rows = d.pcCols, d.pcRows
    }
    d.resizePTY(rows, cols)
    d.sendControlMessage("mode:" + client)
}
```

### Reconnexion WebSocket

Boucle infinie avec backoff:

```go
func (d *Daemon) connectToRelay() {
    for {
        conn, _, err := websocket.Dial(...)
        if err != nil {
            time.Sleep(RelayConnectDelay)
            continue
        }
        d.handleWebSocketMessages(conn)
        // Connexion perdue
        time.Sleep(ReconnectDelay)
    }
}
```

---

## Dependances

| Package | Usage |
|---------|-------|
| `github.com/creack/pty` | Spawn et gestion PTY |
| `github.com/gorilla/websocket` | Client WebSocket |
| `github.com/google/uuid` | Generation UUID |
| `github.com/skip2/go-qrcode` | Generation QR codes |
| `golang.org/x/crypto/curve25519` | X25519 ECDH |
| `golang.org/x/crypto/nacl/box` | Chiffrement NaCl |
| `golang.org/x/term` | Terminal raw mode, taille |
