# Fiber Tracker

Suivi des interventions techniques fibre optique avec tableau de bord web en temps réel, notifications WhatsApp automatisées, et authentification sécurisée via Zitadel (IAM).

## Architecture

Application Go monolithique conteneurisée avec dashboard web embarqué (Vanilla JS), Zitadel pour l'authentification (via Headless API), et PostgreSQL pour la persistance (sessions whatsmeow) :

```text
fiber-tracker/
├── main.go                 # Point d'entrée + injection des dépendances
├── docker-compose.yml      # Orchestration multi-services (App, DB, Zitadel)
├── Dockerfile              # Build multi-stage (Go → Alpine)
├── internal/
│   ├── auth/               # Middleware JWT, sessions Zitadel (Headless API), RBAC
│   ├── config/             # Chargement/sauvegarde de la config JSON (Volume Docker)
│   ├── excel/              # Parsing Excel (.xlsx) et calcul des statistiques
│   ├── gdrive/             # Intégration Google Drive (OAuth2)
│   ├── models/             # Types de données (stats, notifications, interventions)
│   ├── scheduler/          # Planification des tâches en arrière-plan (cron)
│   ├── smtp/               # Mailer SMTP + templates email dark-mode natifs
│   ├── watcher/            # Surveillance dossier d'upload (fsnotify)
│   └── whatsapp/           # Client WhatsApp Web (go-whatsmeow + PostgreSQL)
└── web/
    ├── server.go            # Serveur HTTP `net/http` + API REST + Middlewares
    └── static/
        ├── index.html       # Dashboard SPA (Single Page Application)
        ├── login.html       # Page de connexion customisée
        ├── style.css        # Design system dark premium (CSS variables, flex/grid)
        └── app.js           # Logique frontend, graphiques SVG, requêtes `fetch`
```

## Prérequis

- Docker & Docker Compose
- Serveur Linux (VPS) pour le déploiement de production

## Démarrage (Développement)

```bash
docker compose up -d --build
```

Le dashboard est accessible à **http://localhost:9510**.

### Services Docker

| Service | Container | Port Host | Description |
|---------|-----------|-----------|-------------|
| **App** | `fiber_app` | `9510` | Backend Go + Dashboard Frontend |
| **Database** | `fiber_db` | `5444` | PostgreSQL 16 (Stockage sessions WhatsApp) |
| **Auth** | `zitadel_app` | `8080` | Zitadel IAM (Identity and Access Management) |

## Configuration

La configuration de l'application est divisée en deux parties :

1. **Variables d'environnement (`.env`)** : Fichiers secrets liés à l'infrastructure (`DB_PASSWORD`, `ZITADEL_MACHINEKEY`, `ZITADEL_SERVICE_PAT`). Le backend refuse de démarrer si ces variables sont manquantes. `DEBUG_MODE=true` est requis pour accéder aux routes de test.
2. **Configuration métier (`config.json`)** : Fichier géré via l'UI (`/api/config`) et stocké dans un volume Docker sécurisé (`fiber-tracker_app_config`). Les secrets (`SMTP_PASSWORD`, tokens Drive) sont masqués côté frontend (`********`).

| Clé (Config UI) | Description | Défaut |
|-----|------------|--------|
| `TECHNICIENS` | Mapping "Nom Technicien" → "Numéro WhatsApp" | — |
| `SUPERVISEUR_PHONE` | Numéro du superviseur pour alertes globales | — |
| `MORNING_HOUR` / `MORNING_MINUTE` | Heure du briefing WhatsApp matinal | `7:45` |
| `STATS_INTERVAL_HOURS` / `STATS_INTERVAL_MINUTES` | Intervalle envois statistiques (WhatsApp) | `2:00` |
| `EOD_HOUR` / `EOD_MINUTE` | Heure du récapitulatif fin de journée | `17:00` |
| `WHATSAPP_ENABLED` | Activation de l'envoi de messages WhatsApp | `false` |
| `SMTP_HOST` / `SMTP_PORT` | Serveur SMTP (Alertes & Reset Password) | `smtp.gmail.com:465` |
| `GDRIVE_ENABLED` | Activation du sync Google Drive | `false` |

## Fonctionnalités Principales

### 📊 Dashboard Temps Réel (SPA)
- Frontend Vanilla JS (aucun framework lourd).
- Stats OK/NOK avec jauges circulaires SVG animées.
- Fiches performances par technicien (taux de réussite, RACC/SAV).
- Diagramme de Gantt interactif (Planning).
- Mode "Dark Premium" natif.

### 📁 Gestion & Watcher de Fichiers
- Backend `fsnotify` qui surveille automatiquement les nouveaux `.xlsx` (stockés dans un volume partagé).
- Upload manuel drag & drop via l'UI web.
- Intégration Google Drive optionnelle pour le rapatriement distant automatique.

### ⏰ Planificateur de Tâches (Cron interne)
- Boucle goroutine qui dispatche les alertes selon le calendrier configuré dans `config.json`.
- Envoi automatique des briefings (07:45), des recaps réguliers, et de la clôture (17:00).

### 📱 WhatsApp (go-whatsmeow)
- Connexion via QR Code dynamique dans l'UI des paramètres.
- Persistance de la session Multi-Device dans PostgreSQL (évite les déconnexions).
- Envoi asynchrone des statistiques et alertes.

### 🔐 Sécurité & Authentification (Zitadel)
- Auth "Headless" : utilisation de la *Session API v2* de Zitadel pour authentifier les utilisateurs directement sur `/login.html` sans redirections externes (nécessite un PAT `IAM_OWNER`).
- Middleware propriétaire (`authWrap`) validant les JWT HttpOnly.
- `rateLimitMiddleware` protégeant contre le brute-force, avec détection d'adresse IP (supportant le reverse-proxying Docker).

## API REST

### Endpoints Publics (Auth)

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| POST | `/api/auth/login` | Login Headless via Zitadel (Rate-limited) |
| POST | `/api/auth/logout` | Suppression du JWT HttpOnly |
| POST | `/api/auth/forgot-password` | Envoi d'email de reset (Rate-limited) |
| GET | `/api/auth/me` | Récupération d'identité courante |

### Endpoints Protégés (Nécessite JWT)

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| GET | `/api/stats` | Renvoie le JSON complet des performances du jour |
| GET/PUT | `/api/config` | Lecture ou modification du `config.json` |
| POST | `/api/upload` | Upload d'un rapport `.xlsx` |
| GET | `/api/whatsapp/qr` | Websocket/Polling stream pour afficher le QR Code de pairage |

*(Voir le code source dans `web/server.go` pour la liste exhaustive)*

## Commandes Utiles (Maintenance)

```bash
# Démarrer tous les services en production
docker compose up -d --build

# Voir les logs de l'application Go
docker compose logs -f app

# Redémarrer unitairement le container Go
docker compose restart app

# Purger complètement la base de données et les sessions
docker compose down -v
```

## Licence

© 2026 Moca Consult. Tous droits réservés.
