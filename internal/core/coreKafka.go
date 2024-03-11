package core

import (
	"time"

)

type KafkaConfig struct {
	KafkaConfigurations    	KafkaConfigurations
}

type KafkaConfigurations struct {
    Username		string 
    Password		string 
    Protocol		string
    Mechanisms		string
    Clientid		string 
    Brokers1		string 
    Brokers2		string 
    Brokers3		string 
	Groupid			string 
	Partition       int
    ReplicationFactor int
    RequiredAcks    int
    Lag             int
    LagCommit       int
}

type Event struct {
    ID          int         `json:"id"`
	Key			string      `json:"key"`
    EventDate   time.Time   `json:"event_date"`
    EventType   string      `json:"event_type"`
    EventData   *EventData   `json:"event_data"`
}

type EventData struct {
    Transfer   *Transfer    `json:"transfer"`
}

type Topic struct {
	Credit     string    `json:"topic_credit"`
    Dedit      string    `json:"topic_debit"`
    Transfer   string    `json:"topic_transfer"`
}