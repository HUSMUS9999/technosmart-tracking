# Fiber Tracker

Suivi des interventions techniques fibre optique avec tableau de bord web en temps réel, notifications WhatsApp automatisées, et authentification sécurisée via Zitadel (OIDC).

## Architecture

Application Go conteneurisée avec dashboard web embarqué, Zitadel pour l'authentification, et PostgreSQL pour la persistance :

```
fiber-tracker/
├── main.go                 # Point d'entrée + logique de planification
├── config.json             # Configuration persistée (bind-mount Docker)
├── docker-compose.yml      # Orchestration multi-services
├── Dockerfile              # Build multi-stage (Go → Alpine)
├── internal/
│   ├── auth/               # Middleware JWT, sessions, RBAC
│   ├── config/             # Chargement/sauvegarde config JSON
│   ├── excel/              # Parsing Excel (.xlsx)
│   ├── gdrive/             # Intégration Google Drive
│   ├── models/             # Types de données (stats, notifications)
│   ├── scheduler/          # Planification (cron)
│   ├── smtp/               # Mailer SMTP + templates email dark-mode
│   ├── watcher/            # Surveillance dossier (fsnotify)
│   └── whatsapp/           # Client WhatsApp (go-whatsmeow)
└── web/
    ├── server.go            # Serveur HTTP + API REST + auth middleware
    └── static/
        ├── index.html       # Dashboard SPA
        ├── login.html       # Page de connexion + reset password
        ├── style.css        # Design system dark premium (CSS custom properties)
        └── app.js           # Logique frontend + composants UI custom
```

## Prérequis

- Docker & Docker Compose

## Démarrage

```bash
docker compose up -d --build
```

Le dashboard est accessible à **http://localhost:9510**.

### Services

| Service | Container | Port | Description |
|---------|-----------|------|-------------|
| **App** | `fiber_app` | `9510` | Backend Go + Dashboard |
| **Database** | `fiber_db` | `5444` | PostgreSQL 16 |
| **Auth** | `zitadel_app` | `8080` | Zitadel OIDC Provider |

## Configuration

Le fichier `config.json` est bind-mounté depuis l'hôte et modifiable via le dashboard (Settings → Save). Les secrets sont masqués côté frontend (`********`) et préservés automatiquement lors des sauvegardes.

| Clé | Description | Défaut |
|-----|------------|--------|
| `TECHNICIENS` | Nom → numéro WhatsApp | — |
| `MY_NUMBER` | Votre numéro principal | — |
| `WATCH_FOLDER` | Dossier à surveiller | `/home/hus/Downloads` |
| `MORNING_HOUR` / `MORNING_MINUTE` | Heure message matinal | `7:45` |
| `STATS_INTERVAL_HOURS` / `STATS_INTERVAL_MINUTES` | Intervalle stats | `2:00` |
| `EOD_HOUR` / `EOD_MINUTE` | Heure récap fin de journée | `17:00` |
| `WEB_PORT` | Port du dashboard | `9510` |
| `WHATSAPP_ENABLED` | Activer WhatsApp | `false` |
| `SMTP_HOST` / `SMTP_PORT` | Serveur SMTP | `smtp.gmail.com:465` |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | Identifiants SMTP | — |
| `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | Credentials Zitadel | — |
| `GDRIVE_ENABLED` / `GDRIVE_FOLDER_ID` | Intégration Google Drive | `false` |

## Fonctionnalités

### 📊 Dashboard Temps Réel
- Stats OK/NOK avec jauges circulaires animées
- Fiches techniciens individuelles avec taux de réussite
- Sélection de fichiers Excel depuis le dashboard
- Notifications en temps réel (toast system)

### 📁 Gestion de Fichiers
- Surveillance automatique du dossier configuré (`.xlsx`)
- Upload drag & drop via le dashboard
- Intégration Google Drive (OAuth2) pour sync automatique

### ⏰ Messages Planifiés
- Message matinal configurable (heure exacte HH:MM)
- Stats périodiques (intervalle configurable)
- Récap fin de journée avec remerciements personnalisés
- Variables dynamiques (`{prenom}`) dans les templates

### 📱 WhatsApp
- Connexion via QR code directement dans le dashboard
- Envoi de notifications automatisées aux techniciens
- Test d'envoi intégré
- Déconnexion sécurisée

### 🔐 Authentification & Sécurité
- Authentification OIDC via Zitadel
- RBAC (admin / viewer)
- Rate limiting sur les endpoints d'auth (5 tentatives/min)
- Réinitialisation de mot de passe par email
- Secrets masqués dans les réponses API (`SMTP_PASSWORD`, `OIDC_CLIENT_SECRET`, etc.)
- Protection path traversal sur les uploads

### 📧 Emails SMTP
- Templates email dark-mode natives (résistent à l'inversion Gmail/Outlook)
- Email de test SMTP intégré
- Email de réinitialisation de mot de passe avec CTA
- Logo Moca Consult embarqué (CID inline)

### 🎨 Interface Premium
- Design system dark-mode avec CSS custom properties
- Time pickers FluxUI-inspired (dual-column heures/minutes)
- Boutons de sauvegarde contextuels par section (activés au changement)
- Animations micro-interactions et transitions fluides
- Responsive mobile

## API REST

### Endpoints Publics (auth)

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| POST | `/api/auth/login` | Connexion locale (rate limited) |
| GET | `/api/auth/login/oidc` | Initier le flux OIDC |
| GET | `/api/auth/callback` | Callback OIDC |
| POST | `/api/auth/logout` | Déconnexion |
| POST | `/api/auth/forgot-password` | Demande de reset (rate limited) |
| POST | `/api/auth/reset-password` | Reset mot de passe |
| GET | `/api/auth/me` | Utilisateur courant |

### Endpoints Protégés (JWT required)

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| GET | `/api/stats` | Stats actuelles |
| GET | `/api/status` | État du serveur |
| GET/PUT | `/api/config` | Lecture/modification config (PUT = admin) |
| GET | `/api/notifications` | Historique notifications |
| POST | `/api/upload` | Upload Excel (admin) |
| POST | `/api/parse` | Parser un fichier (admin) |
| GET | `/api/time` | Heure France (NTP/HTTP fallback) |
| GET | `/api/files` | Liste des fichiers Excel |
| POST | `/api/files/select` | Sélectionner un fichier actif |

### Endpoints WhatsApp

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| GET | `/api/whatsapp/status` | Statut connexion |
| GET | `/api/whatsapp/qr` | Générer QR code |
| POST | `/api/whatsapp/send` | Envoyer un message |
| POST | `/api/whatsapp/logout` | Déconnecter |

### Endpoints Google Drive

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| GET | `/api/drive/status` | Statut connexion Drive |
| GET | `/api/drive/auth-url` | URL d'autorisation OAuth2 |
| GET | `/api/drive/callback` | Callback OAuth2 |
| POST | `/api/drive/disconnect` | Déconnecter Drive |
| GET | `/api/drive/folders` | Lister les dossiers |
| GET | `/api/drive/files` | Lister les fichiers |
| POST | `/api/drive/sync` | Synchroniser maintenant |
| POST | `/api/drive/set-folder` | Configurer le dossier source |

### Endpoints SMTP

| Méthode | Endpoint | Description |
|---------|----------|-------------|
| GET/POST | `/api/smtp/zitadel` | Lire/configurer SMTP Zitadel |
| POST | `/api/smtp/zitadel/test` | Envoyer un email de test |
| POST | `/api/smtp/test` | Test SMTP direct |

## Commandes Utiles

```bash
# Démarrer tous les services
docker compose up -d --build

# Reconstruire uniquement l'app
docker compose up -d --build app

# Redémarrer tous les services
docker compose restart

# Voir les logs en temps réel
docker compose logs -f app

# Arrêter tout
docker compose down

# Arrêter et supprimer les volumes (reset DB)
docker compose down -v
```

## Licence

© 2026 Moca Consult. Tous droits réservés.
