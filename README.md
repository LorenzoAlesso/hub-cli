# hub-cli

CLI that automates Build → Push → Deploy → git commit on Kubernetes clusters backed by AWS ECR.

It handles ECR login, Docker build, image push, and Helm upgrade for one or more services in sequence, with automatic tag versioning.

## Requirements

- `docker`, `kubectl`, `helm`, `aws` available in `PATH`
- AWS ECR access configured

## Installation

Copy the executable to a folder in `PATH`:

```powershell
# Windows
Copy-Item .\hub-cli.exe "$env:USERPROFILE\AppData\Local\Microsoft\WindowsApps\hub-cli.exe"
```

On first run, the config file is created at `~/.hub-cli.yaml`.

## Quick start

```
hub-cli                    # run the deploy workflow
hub-cli config show        # show current configuration
hub-cli config set-theme   # change the interface theme
```

See [docs/Instructions.md](docs/Instructions.md) for the full command reference and configuration details.

---

# hub-cli (italiano)

CLI per automatizzare il ciclo Build → Push → Deploy, e commit git, su cluster Kubernetes con ECR (AWS).

Gestisce il login ECR, la build Docker, il push e l'upgrade Helm per uno o più servizi in sequenza, con versionamento automatico dei tag.

## Prerequisiti

- `docker`, `kubectl`, `helm`, `aws` disponibili nel `PATH`
- Accesso configurato ad AWS ECR

## Installazione

Copia l'eseguibile in una cartella nel `PATH`:

```powershell
# Windows
Copy-Item .\hub-cli.exe "$env:USERPROFILE\AppData\Local\Microsoft\WindowsApps\hub-cli.exe"
```

Al primo avvio il file di configurazione viene creato automaticamente in `~/.hub-cli.yaml`.

## Utilizzo rapido

```
hub-cli                    # avvia il workflow di deploy
hub-cli config show        # mostra la configurazione corrente
hub-cli config set-theme   # cambia tema dell'interfaccia
```

Vedi [docs/Instructions.md](docs/Instructions.md) per il riferimento completo dei comandi e la configurazione.
