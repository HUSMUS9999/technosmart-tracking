# Fiber Tracker

Suivi des interventions techniques fibre optique avec tableau de bord web en temps réel.

## Architecture

Application Go monolithique avec dashboard web embarqué :

```
fiber-tracker/
├── main.go                 # Point d'entrée
├── config.json             # Configuration
├── internal/
│   ├── config/             # Chargement config
│   ├── excel/              # Parsing Excel
│   ├── models/             # Types de données
│   ├── scheduler/          # Planification (cron)
│   ├── watcher/            # Surveillance dossier
│   └── whatsapp/           # Client WhatsApp (stub)
└── web/
    ├── server.go            # Serveur HTTP + API REST
    └── static/
        ├── index.html       # Dashboard SPA
        ├── style.css        # Thème dark premium
        └── app.js           # Logique frontend
```

## Prérequis

- Go 1.21+

## Installation

```bash
go mod tidy
go build -o fiber-tracker .
```

## Démarrage

```bash
./fiber-tracker
```

Le dashboard est accessible à **http://localhost:8080**.

## Configuration

Éditer `config.json` :

| Clé | Description | Défaut |
|-----|------------|--------|
| `TECHNICIENS` | Nom → numéro WhatsApp | — |
| `MY_NUMBER` | Votre numéro | — |
| `WATCH_FOLDER` | Dossier à surveiller | `/home/hus/Downloads` |
| `MORNING_HOUR` | Heure message matinal | `8` |
| `STATS_INTERVAL_HOURS` | Intervalle stats (h) | `2` |
| `WEB_PORT` | Port du dashboard | `8080` |
| `WHATSAPP_ENABLED` | Activer WhatsApp | `false` |

## Fonctionnalités

- 📊 **Dashboard temps réel** — Stats OK/NOK, jauge circulaire, fiches techniciens
- 📁 **Surveillance dossier** — Détection auto des nouveaux fichiers `.xlsx`
- ⬆️ **Import fichier** — Upload via le dashboard web (drag & drop)
- ⏰ **Messages planifiés** — Stats toutes les 2h, message matinal, récap fin de journée
- 👷 **Vue techniciens** — Cartes individuelles avec taux de réussite
- 📱 **WhatsApp** — Envoi de notifications (à activer dans config)

## API REST

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| GET | `/api/stats` | Stats actuelles |
| GET | `/api/status` | État du serveur |
| GET | `/api/config` | Configuration |
| PUT | `/api/config` | Modifier la config |
| GET | `/api/notifications` | Historique notifications |
| POST | `/api/upload` | Upload Excel |
| POST | `/api/parse` | Parser un fichier |
