package model

type Results struct {
	Position       string `json:"position"`
	Mark           string `json:"mark"`
	WinnerLoserTie string `json:"winnerLoserTie"`
	MedalType      string `json:"medalType"`
	IRM            string `json:"irm"`
}

type Competitor struct {
	Code    string  `json:"code"`
	NOC     string  `json:"noc"`
	Name    string  `json:"name"`
	Order   int     `json:"order"`
	Results Results `json:"results"`
}

type ExtraData struct {
	DetailURL string `json:"detailUrl"`
}

type Unit struct {
	DisciplineName      string       `json:"disciplineName"`
	EventUnitName       string       `json:"eventUnitName"`
	ID                  string       `json:"id"`
	DisciplineCode      string       `json:"disciplineCode"`
	GenderCode          string       `json:"genderCode"`
	EventCode           string       `json:"eventCode"`
	PhaseCode           string       `json:"phaseCode"`
	EventID             string       `json:"eventId"`
	EventName           string       `json:"eventName"`
	PhaseID             string       `json:"phaseId"`
	PhaseName           string       `json:"phaseName"`
	DisciplineID        string       `json:"disciplineId"`
	EventOrder          int          `json:"eventOrder"`
	PhaseType           string       `json:"phaseType"`
	EventUnitType       string       `json:"eventUnitType"`
	OlympicDay          string       `json:"olympicDay"`
	StartDate           string       `json:"startDate"`
	EndDate             string       `json:"endDate"`
	HideStartDate       bool         `json:"hideStartDate"`
	HideEndDate         bool         `json:"hideEndDate"`
	StartText           string       `json:"startText"`
	Order               int          `json:"order"`
	Venue               string       `json:"venue"`
	VenueDescription    string       `json:"venueDescription"`
	Location            string       `json:"location"`
	LocationDescription string       `json:"locationDescription"`
	Status              string       `json:"status"`
	StatusDescription   string       `json:"statusDescription"`
	MedalFlag           int          `json:"medalFlag"`
	LiveFlag            bool         `json:"liveFlag"`
	ScheduleItemType    string       `json:"scheduleItemType"`
	UnitNum             string       `json:"unitNum"`
	SessionCode         string       `json:"sessionCode"`
	GroupID             string       `json:"groupId"`
	Competitors         []Competitor `json:"competitors"`
	ExtraData           ExtraData    `json:"extraData"`
}

type Response struct {
	Units []Unit `json:"units"`
}
