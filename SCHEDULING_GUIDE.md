# 📅 Guide du Scheduling de Bande Passante

## Vue d'ensemble

Le système de scheduling permet de **programmer automatiquement** des changements de bande passante à des heures spécifiques en utilisant des **expressions cron**.

### Fonctionnalités
- ✅ Scheduling basé sur cron (flexible et puissant)
- ✅ Règles multiples avec priorités
- ✅ Durée configurable pour chaque règle
- ✅ Activation/désactivation individuelle des règles
- ✅ Consultation de la prochaine exécution

---

## 📝 Expressions Cron

Format: `minute heure jour mois jour_semaine`

### Exemples courants

| Expression | Description |
|------------|-------------|
| `0 8 * * 1-5` | 8h00 du lundi au vendredi (jours de semaine) |
| `0 22 * * 0,6` | 22h00 le dimanche et samedi (weekend) |
| `0 */2 * * *` | Toutes les 2 heures |
| `0 0 */2 * *` | Tous les 2 jours à minuit |
| `0 18 * * *` | Tous les jours à 18h00 |
| `30 9 * * 1` | 9h30 tous les lundis |
| `0 12,18 * * *` | À midi et 18h00 tous les jours |
| `*/15 * * * *` | Toutes les 15 minutes |

### Jours de la semaine
- `0` = Dimanche
- `1` = Lundi
- `2` = Mardi
- `3` = Mercredi
- `4` = Jeudi
- `5` = Vendredi
- `6` = Samedi

---

## 🔌 API Endpoints

### 1. Obtenir toutes les règles
```bash
GET /qos/schedule/global
```

**Réponse:**
```json
{
  "rules": [
    {
      "id": "work-hours",
      "name": "Heures de travail",
      "description": "Limite pendant les heures de bureau",
      "rate_mbps": 50,
      "cron_expr": "0 8 * * 1-5",
      "duration": 600,
      "enabled": true
    }
  ]
}
```

### 2. Définir des règles (remplace toutes)
```bash
POST /qos/schedule/global
Content-Type: application/json

{
  "rules": [
    {
      "id": "work-hours",
      "name": "Heures de travail",
      "description": "50 Mbps de 8h à 18h en semaine",
      "rate_mbps": 50,
      "cron_expr": "0 8 * * 1-5",
      "duration": 600,
      "enabled": true
    },
    {
      "id": "evening",
      "name": "Soirée",
      "description": "80 Mbps après 18h",
      "rate_mbps": 80,
      "cron_expr": "0 18 * * *",
      "duration": 360,
      "enabled": true
    }
  ]
}
```

### 3. Ajouter une règle
```bash
POST /qos/schedule/global/rule
Content-Type: application/json

{
  "id": "night-low",
  "name": "Limite nocturne",
  "description": "Bande passante réduite la nuit",
  "rate_mbps": 20,
  "cron_expr": "0 1 * * *",
  "duration": 420,
  "enabled": true
}
```

### 4. Supprimer une règle
```bash
DELETE /qos/schedule/global/work-hours
```

### 5. Voir la prochaine exécution
```bash
GET /qos/schedule/global/work-hours/next
```

**Réponse:**
```json
{
  "rule_id": "work-hours",
  "next_time": "2025-12-12 08:00:00",
  "next_unix": 1734001200
}
```

---

## 💡 Exemples pratiques

### Scénario 1: Bureau (8h-18h lun-ven)
Limitation à 50 Mbps pendant les heures de bureau.

```bash
curl -X POST http://localhost:8080/qos/schedule/global \
  -H 'Content-Type: application/json' \
  -d '{
    "rules": [{
      "id": "office-hours",
      "name": "Heures de bureau",
      "description": "Limite de 50 Mbps de 8h à 18h du lundi au vendredi",
      "rate_mbps": 50,
      "cron_expr": "0 8 * * 1-5",
      "duration": 600,
      "enabled": true
    }]
  }'
```

**Explication:**
- `cron_expr: "0 8 * * 1-5"` → 8h00 du lundi (1) au vendredi (5)
- `duration: 600` → Applique pendant 600 minutes (10 heures)
- À 8h00, le système applique 50 Mbps
- Après 10h (18h00), la limite expire

### Scénario 2: Weekend illimité
100 Mbps le weekend.

```bash
curl -X POST http://localhost:8080/qos/schedule/global/rule \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "weekend-boost",
    "name": "Weekend full speed",
    "description": "100 Mbps le samedi et dimanche",
    "rate_mbps": 100,
    "cron_expr": "0 0 * * 6,0",
    "duration": 1440,
    "enabled": true
  }'
```

**Explication:**
- `cron_expr: "0 0 * * 6,0"` → Minuit le samedi (6) et dimanche (0)
- `duration: 1440` → 24 heures (1440 minutes)

### Scénario 3: Heures creuses toutes les 2 nuits
Limite réduite tous les 2 jours la nuit.

```bash
curl -X POST http://localhost:8080/qos/schedule/global/rule \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "night-every-2-days",
    "name": "Nuit tous les 2 jours",
    "description": "20 Mbps la nuit tous les 2 jours",
    "rate_mbps": 20,
    "cron_expr": "0 1 */2 * *",
    "duration": 420,
    "enabled": true
  }'
```

**Explication:**
- `cron_expr: "0 1 */2 * *"` → 1h00 tous les 2 jours
- `duration: 420` → 7 heures (jusqu'à 8h00)

### Scénario 4: Pics de midi et soir
Limite différente à midi et le soir.

```bash
curl -X POST http://localhost:8080/qos/schedule/global \
  -H 'Content-Type: application/json' \
  -d '{
    "rules": [
      {
        "id": "lunch-time",
        "name": "Pause déjeuner",
        "rate_mbps": 30,
        "cron_expr": "0 12 * * *",
        "duration": 60,
        "enabled": true
      },
      {
        "id": "dinner-time",
        "name": "Dîner",
        "rate_mbps": 40,
        "cron_expr": "0 19 * * *",
        "duration": 120,
        "enabled": true
      }
    ]
  }'
```

### Scénario 5: Heures de pointe toutes les 4 heures
```bash
curl -X POST http://localhost:8080/qos/schedule/global/rule \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "every-4h",
    "name": "Toutes les 4 heures",
    "rate_mbps": 60,
    "cron_expr": "0 */4 * * *",
    "duration": 120,
    "enabled": true
  }'
```

---

## 🧪 Tests

### 1. Vérifier les règles actives
```bash
curl http://localhost:8080/qos/schedule/global
```

### 2. Voir la prochaine exécution
```bash
curl http://localhost:8080/qos/schedule/global/office-hours/next
```

### 3. Désactiver temporairement une règle
Récupérer les règles, modifier `enabled: false`, puis renvoyer:

```bash
# 1. Récupérer
curl http://localhost:8080/qos/schedule/global > rules.json

# 2. Éditer rules.json: mettre "enabled": false

# 3. Réappliquer
curl -X POST http://localhost:8080/qos/schedule/global \
  -H 'Content-Type: application/json' \
  -d @rules.json
```

### 4. Supprimer toutes les règles
```bash
curl -X POST http://localhost:8080/qos/schedule/global \
  -H 'Content-Type: application/json' \
  -d '{"rules": []}'
```

---

## 🔍 Logs et Monitoring

Le scheduler affiche des logs:
```
[Scheduler] Bandwidth scheduler started
[Scheduler] Rules updated: 2 active rules
[Scheduler] Rule 'Heures de bureau' scheduled with cron: 0 8 * * 1-5
[Scheduler] Executing rule: Heures de bureau (rate: 50 Mbps, duration: 600 min)
[Scheduler] ✓ Applied 50 Mbps for rule 'Heures de bureau'
[Scheduler] Duration expired for rule 'Heures de bureau'...
```

---

## 📚 Référence complète

### Structure ScheduleRule

| Champ | Type | Obligatoire | Description |
|-------|------|-------------|-------------|
| `id` | string | ✅ | Identifiant unique |
| `name` | string | ✅ | Nom lisible |
| `description` | string | ❌ | Description optionnelle |
| `rate_mbps` | int | ✅ | Débit en Mbps (> 0) |
| `cron_expr` | string | ✅ | Expression cron valide |
| `duration` | int | ✅ | Durée en minutes |
| `enabled` | bool | ✅ | Activer/désactiver |

### Notes importantes

1. **Durée**: Après `duration` minutes, la règle expire automatiquement
2. **Priorité**: La première règle qui s'exécute applique son débit
3. **Validation**: Le backend vérifie la syntaxe cron à l'ajout
4. **Persistence**: Les règles sont en mémoire (redémarrage = perte)
5. **Conflits**: Si plusieurs règles se déclenchent simultanément, la première gagne

---

## 🚀 Quick Start

**Tester maintenant (règle toutes les 2 minutes):**

```bash
curl -X POST http://localhost:8080/qos/schedule/global/rule \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "test-2min",
    "name": "Test toutes les 2 minutes",
    "rate_mbps": 30,
    "cron_expr": "*/2 * * * *",
    "duration": 1,
    "enabled": true
  }'
```

Attendez 2 minutes et vérifiez les logs ! ✅

---

## 📞 Support

Pour tester vos expressions cron:
- https://crontab.guru/
- https://crontab.cronhub.io/

**Date:** 11 décembre 2025  
**Version:** 1.0
