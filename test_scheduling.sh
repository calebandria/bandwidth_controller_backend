#!/bin/bash
# Script de test du scheduling de bande passante

API_BASE="http://localhost:8080"

echo "=========================================="
echo "🧪 Test du système de scheduling"
echo "=========================================="
echo ""

# Test 1: Vérifier que le serveur répond
echo "1️⃣ Vérification du serveur..."
if curl -s -o /dev/null -w "%{http_code}" "${API_BASE}/swagger/index.html" | grep -q "200"; then
    echo "✅ Serveur OK"
else
    echo "❌ Serveur non accessible"
    exit 1
fi
echo ""

# Test 2: Créer une règle de test (toutes les 2 minutes)
echo "2️⃣ Création d'une règle de test (se déclenche toutes les 2 minutes)..."
curl -X POST "${API_BASE}/qos/schedule/global/rule" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "test-2min",
    "name": "Test automatique",
    "description": "Applique 30 Mbps toutes les 2 minutes pendant 1 minute",
    "rate_mbps": 30,
    "cron_expr": "*/2 * * * *",
    "duration": 1,
    "enabled": true
  }'
echo -e "\n"

# Test 3: Lister toutes les règles
echo "3️⃣ Liste des règles actives..."
curl -s "${API_BASE}/qos/schedule/global" | jq '.'
echo ""

# Test 4: Voir la prochaine exécution
echo "4️⃣ Prochaine exécution de la règle test-2min..."
curl -s "${API_BASE}/qos/schedule/global/test-2min/next" | jq '.'
echo ""

# Test 5: Créer plusieurs règles pour démonstration
echo "5️⃣ Ajout de règles pour scénarios réels..."

# Règle bureau (8h-18h lundi-vendredi)
echo "   📋 Heures de bureau (8h-18h lun-ven, 50 Mbps)..."
curl -s -X POST "${API_BASE}/qos/schedule/global/rule" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "work-hours",
    "name": "Heures de travail",
    "description": "Limite à 50 Mbps pendant les heures de bureau",
    "rate_mbps": 50,
    "cron_expr": "0 8 * * 1-5",
    "duration": 600,
    "enabled": true
  }' > /dev/null
echo "   ✅ Ajouté"

# Règle soirée (18h tous les jours)
echo "   🌙 Soirée (18h, 80 Mbps)..."
curl -s -X POST "${API_BASE}/qos/schedule/global/rule" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "evening",
    "name": "Soirée",
    "description": "80 Mbps après 18h",
    "rate_mbps": 80,
    "cron_expr": "0 18 * * *",
    "duration": 360,
    "enabled": true
  }' > /dev/null
echo "   ✅ Ajouté"

# Règle weekend
echo "   🎉 Weekend (samedi-dimanche, 100 Mbps)..."
curl -s -X POST "${API_BASE}/qos/schedule/global/rule" \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "weekend",
    "name": "Weekend boost",
    "description": "100 Mbps le weekend",
    "rate_mbps": 100,
    "cron_expr": "0 0 * * 6,0",
    "duration": 1440,
    "enabled": true
  }' > /dev/null
echo "   ✅ Ajouté"
echo ""

# Test 6: Afficher toutes les règles
echo "6️⃣ Récapitulatif de toutes les règles configurées..."
curl -s "${API_BASE}/qos/schedule/global" | jq '.rules[] | {id, name, rate_mbps, cron_expr, enabled}'
echo ""

echo "=========================================="
echo "✅ Tests terminés !"
echo "=========================================="
echo ""
echo "💡 Pour monitorer les exécutions automatiques:"
echo "   tail -f /tmp/qos-scheduler.log"
echo ""
echo "💡 Pour supprimer une règle:"
echo "   curl -X DELETE ${API_BASE}/qos/schedule/global/test-2min"
echo ""
echo "💡 Dans 2 minutes, la règle 'test-2min' s'exécutera automatiquement."
echo "   Vous verrez dans les logs:"
echo "   [Scheduler] Executing rule: Test automatique (rate: 30 Mbps, duration: 1 min)"
echo ""
