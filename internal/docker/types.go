package docker

import (
	"fmt"
	"math"
	"time"

	"github.com/amir20/dozzle/internal/utils"
)

// Container represents an internal representation of docker containers
type Container struct {
	ID        string                           `json:"id"`
	Name      string                           `json:"name"`
	Image     string                           `json:"image"`
	ImageID   string                           `json:"imageId"`
	Command   string                           `json:"command"`
	Created   time.Time                        `json:"created"`
	StartedAt time.Time                        `json:"startedAt,omitempty"`
	State     string                           `json:"state"`
	Health    string                           `json:"health,omitempty"`
	Host      string                           `json:"host,omitempty"`
	Tty       bool                             `json:"-"`
	Labels    map[string]string                `json:"labels,omitempty"`
	Stats     *utils.RingBuffer[ContainerStat] `json:"stats,omitempty"`
	Group     string                           `json:"group,omitempty"`
}

// ContainerStat represent stats instant for a container
type ContainerStat struct {
	ID            string  `json:"id"`
	CPUPercent    float64 `json:"cpu"`
	MemoryPercent float64 `json:"memory"`
	MemoryUsage   float64 `json:"memoryUsage"`
}

// ContainerEvent represents events that are triggered
type ContainerEvent struct {
	ActorID string `json:"actorId"`
	Name    string `json:"name"`
	Host    string `json:"host"`
}

type LogPosition string

const (
	Beginning LogPosition = "start"
	Middle    LogPosition = "middle"
	End       LogPosition = "end"
)

type ContainerAction string

const (
	Start   ContainerAction = "start"
	Stop    ContainerAction = "stop"
	Restart ContainerAction = "restart"
)

func ParseContainerAction(input string) (ContainerAction, error) {
	action := ContainerAction(input)
	switch action {
	case Start, Stop, Restart:
		return action, nil
	default:
		return "", fmt.Errorf("unknown action: %s", input)
	}
}

type LogEvent struct {
	Message     any         `json:"m,omitempty"`
	Timestamp   int64       `json:"ts"`
	Id          uint32      `json:"id,omitempty"`
	Level       string      `json:"l,omitempty"`
	Position    LogPosition `json:"p,omitempty"`
	Stream      string      `json:"s,omitempty"`
	ContainerID string      `json:"c,omitempty"`
}

func (l *LogEvent) HasLevel() bool {
	return l.Level != ""
}

func (l *LogEvent) IsCloseToTime(other *LogEvent) bool {
	return math.Abs(float64(l.Timestamp-other.Timestamp)) < 10
}
