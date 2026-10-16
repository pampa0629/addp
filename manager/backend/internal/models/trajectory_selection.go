package models

// TrajectorySelection is a bounded selection inside one scanned file item.
type TrajectorySelection struct {
	DeviceField  string `json:"device_field"`
	TimeField    string `json:"time_field"`
	TimeEncoding string `json:"time_encoding"`
	Devices      []any  `json:"devices"`
	Start        string `json:"start"`
	End          string `json:"end"`
}

type TrajectoryScope struct {
	Selection   TrajectorySelection `json:"selection"`
	Complete    bool                `json:"complete"`
	ScannedRows int                 `json:"scanned_rows"`
}
