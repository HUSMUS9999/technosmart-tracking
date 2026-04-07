package models

import "time"

// TechStats holds per-technician statistics.
type TechStats struct {
	Name    string  `json:"name"`
	OK      int     `json:"ok"`
	NOK     int     `json:"nok"`
	Total   int     `json:"total"`
	RateOK  float64 `json:"rate_ok"`
	RACC_OK int     `json:"racc_ok"`
	RACC_NOK int    `json:"racc_nok"`
	SAV_OK  int     `json:"sav_ok"`
	SAV_NOK int     `json:"sav_nok"`
	Sector  string  `json:"sector"`
}

// NOKRecord represents a single failed intervention.
type NOKRecord struct {
	Tech      string `json:"tech"`
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
	Department string `json:"department"`
	PM        string `json:"pm"`
	Duration  string `json:"duration"`
	FailCode  string `json:"fail_code"`
	Category  string `json:"category"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// InterventionRecord is a full row from "Tableau récap"
type InterventionRecord struct {
	Reference  string `json:"reference"`
	StartTime  string `json:"start_time"`
	EndTime    string `json:"end_time"`
	Finished   string `json:"finished"`
	State      string `json:"state"`
	Department string `json:"department"`
	PM         string `json:"pm"`
	Tech       string `json:"tech"`
	Type       string `json:"type"`
	Duration   string `json:"duration"`
	RDVDate    string `json:"rdv_date"`
	Zone       string `json:"zone"`
	ZoneType   string `json:"zone_type"`
	Delay      string `json:"delay"`
	DelayType  string `json:"delay_type"`
	PTOStatus  string `json:"pto_status"`
	PhotoCtrl  string `json:"photo_ctrl"`
	FailCode   string `json:"fail_code"`
	FailDiag   string `json:"fail_diag"`
	FailCat    string `json:"fail_cat"`
}

// GanttSlot represents a tech's hourly slot from the GANTT sheet
type GanttSlot struct {
	Tech    string   `json:"tech"`
	Sector  string   `json:"sector"`
	Slots   []string `json:"slots"`
	Rate    float64  `json:"rate"`
	FillRate float64 `json:"fill_rate"`
}

// DailyStats is the aggregate view returned by the API.
type DailyStats struct {
	Date         string               `json:"date"`
	TotalOK      int                  `json:"total_ok"`
	TotalNOK     int                  `json:"total_nok"`
	Total        int                  `json:"total"`
	RateOK       float64              `json:"rate_ok"`
	RACC_OK      int                  `json:"racc_ok"`
	RACC_NOK     int                  `json:"racc_nok"`
	RACC_Rate    float64              `json:"racc_rate"`
	SAV_OK       int                  `json:"sav_ok"`
	SAV_NOK      int                  `json:"sav_nok"`
	SAV_Rate     float64              `json:"sav_rate"`
	InProgress   int                  `json:"in_progress"`
	AtRisk       int                  `json:"at_risk"`
	Remaining    int                  `json:"remaining"`
	PDC          int                  `json:"pdc"`
	ByTechnician []TechStats          `json:"by_technician"`
	NOKRecords   []NOKRecord          `json:"nok_records"`
	AllRecords   []InterventionRecord `json:"all_records"`
	GanttData    []GanttSlot          `json:"gantt_data"`
	SourceFile   string               `json:"source_file"`
	UpdatedAt    time.Time            `json:"updated_at"`
	// Failure analysis
	FailuresByCategory map[string]int `json:"failures_by_category"`
	FailuresByType     map[string]int `json:"failures_by_type"`
	// Department breakdown
	ByDepartment       map[string]int `json:"by_department"`
	// Zone breakdown
	ByZone             map[string]int `json:"by_zone"`
}

// NotificationLog records a sent message.
type NotificationLog struct {
	ID        int       `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Recipient string    `json:"recipient"`
	Message   string    `json:"message"`
	Success   bool      `json:"success"`
}
