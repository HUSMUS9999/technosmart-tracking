package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

func main() {
	msg := `📋 *Moca Consult — Rapport Superviseur*
📅 2026-04-13
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📊 *Vue d'ensemble:*
🔧 Total interventions: *195*
✅ Réussies: *150*
❌ Échouées: *45*
📈 Taux de réussite: *80%*

👥 *Performance équipe:*
🟢 AMMARI Amazich: 5/5 (100%)
🟢 BENMOUSSA Ilies: 5/6 (83%)
🟢 BENRAMDANE Karim: 5/6 (83%)
🟢 BOURAOUI Saber: 5/6 (83%)
🟢 GANNA Yacine: 5/6 (83%)
🟡 HAMDI Mohamed: 3/5 (60%)
🟢 KLARI Mohamed: 5/6 (83%)
🟢 TEBABES BRAHMI Abdenaceur: 5/6 (83%)
🟢 AMMAN Khalid: 5/5 (100%)
🟢 HAZEM Ahmed: 5/5 (100%)
🟡 AZAIZIA Mohamed Ali: 2/3 (67%)
🟢 BENKHALFOUNE Abdelhak: 5/5 (100%)
🟡 BOUGUETTAYA Akli: 3/4 (75%)
🟡 DAOUDI Mouatez: 3/4 (75%)
🔴 HAMDI Hassen: 0/1 (0%)
🟡 ILTACHE Hocine: 2/3 (67%)
🟡 JILALI Khalifa: 4/8 (50%)
🟢 KHELIF Fateh: 3/3 (100%)
🟢 MAHDAOUIA Rayane: 4/5 (80%)
🔴 MEZRIGUI Moez: 0/1 (0%)
🟡 SADI Samir: 3/5 (60%)
🔴 AICHOUCHE Maamar: 1/3 (33%)
🟢 BOKZINI Abderrahmane: 6/6 (100%)
🟢 ISSAM Mohamed: 5/6 (83%)
🟢 AMARI Nassim: 5/5 (100%)
🟢 BESSADI Mouhamed: 3/3 (100%)
🟢 HADJI Kaced: 2/2 (100%)
🟡 TILKOUT Mohand: 2/3 (67%)
🟡 Ghazi Jerniti Yassine: 4/6 (67%)
🟢 HAMMI Amazigh: 6/7 (86%)
🟢 SKENDRAOUI Sid Ali: 5/6 (83%)
🟡 IBRICHE Hamza: 3/6 (50%)
🟢 ALIANE Arslane: 6/6 (100%)
🟢 BELARBI Mohamed: 5/6 (83%)
🟡 BERKANE Moussa: 3/6 (50%)
🟢 CHABANE Mohamed: 5/6 (83%)
🟢 CHAIR Slimane: 5/5 (100%)
🟡 KACI Sofiane: 2/4 (50%)
🟡 OUAFI Yacine: 4/6 (67%)
🔴 SELLAMI Tarek: 1/5 (20%)

⚠️ *Points d'attention:*
⚡ HAMDI Mohamed — 2 NOK (60% réussite)
⚡ AZAIZIA Mohamed Ali — 1 NOK (67% réussite)
⚡ HAMDI Hassen — 1 NOK (0% réussite)
⚡ ILTACHE Hocine — 1 NOK (67% réussite)
⚡ JILALI Khalifa — 4 NOK (50% réussite)
⚡ MEZRIGUI Moez — 1 NOK (0% réussite)
⚡ SADI Samir — 2 NOK (60% réussite)
⚡ AICHOUCHE Maamar — 2 NOK (33% réussite)
⚡ TILKOUT Mohand — 1 NOK (67% réussite)
⚡ Ghazi Jerniti Yassine — 2 NOK (67% réussite)
⚡ IBRICHE Hamza — 3 NOK (50% réussite)
⚡ BERKANE Moussa — 3 NOK (50% réussite)
⚡ KACI Sofiane — 2 NOK (50% réussite)
⚡ OUAFI Yacine — 2 NOK (67% réussite)
⚡ SELLAMI Tarek — 4 NOK (20% réussite)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔄 Prochain rapport dans 2h`

	payload := map[string]string{
		"to":      "+33764474619",
		"message": msg,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "http://127.0.0.1:9510/api/whatsapp/send", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "ft_session=supersession")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Error sending:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Println("SUCCESS")
	} else {
		fmt.Println("HTTP", resp.StatusCode)
	}
}
