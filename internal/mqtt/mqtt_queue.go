package mqtt

import (
	"encoding/json"
	"strings"
	"time"
)

// commandQueue remembers at most one pending value per MQTT command topic.
// New slider values replace queued values instead of leaving a radio backlog.
// A single command may be in flight; the MQTT network loop never waits for it.
type queuedCommand struct {
	Topic, Payload string
	First, Last    time.Time
}

type recentCommand struct {
	Payload string
	When    time.Time
}

type commandQueue struct {
	pending                        map[string]queuedCommand
	recent                         map[string]recentCommand
	active                         *queuedCommand
	debounce, maxWait, dedupWindow time.Duration
}

func newMQTTCommandQueue(debounce, maxWait, dedup time.Duration) *commandQueue {
	// A configured 0 disables the corresponding delay / duplicate window.
	if debounce < 0 {
		debounce = 0
	}
	if maxWait < debounce {
		maxWait = debounce
	}
	if dedup < 0 {
		dedup = 0
	}
	return &commandQueue{pending: map[string]queuedCommand{}, recent: map[string]recentCommand{}, debounce: debounce, maxWait: maxWait, dedupWindow: dedup}
}

func (q *commandQueue) busy() bool { return q.active != nil }

func isPowerOff(topic, payload string) bool {
	if strings.HasSuffix(topic, "/power/set") {
		return strings.EqualFold(strings.TrimSpace(payload), "OFF")
	}
	if strings.HasSuffix(topic, "/ha/set") {
		var cmd struct {
			State string `json:"state"`
		}
		return json.Unmarshal([]byte(payload), &cmd) == nil && cmd.State == "OFF"
	}
	return false
}

func isPowerTopic(topic, payload string) bool {
	return strings.HasSuffix(topic, "/power/set") || isPowerOff(topic, payload)
}

// offer returns true when the desired command changes. Duplicate HA events do
// not reset debounce. In-flight changes are superseded by only the newest value.
func (q *commandQueue) offer(topic, value string, now time.Time) bool {
	value = strings.TrimSpace(value)
	if q.active != nil && q.active.Topic == topic && q.active.Payload == value {
		// The operator returned to the value already being transmitted.
		delete(q.pending, topic)
		return false
	}
	if existing, ok := q.pending[topic]; ok && existing.Payload == value {
		return false
	}
	if q.active == nil || q.active.Topic != topic {
		if prev, ok := q.recent[topic]; q.dedupWindow > 0 && ok && prev.Payload == value && now.Sub(prev.When) < q.dedupWindow {
			delete(q.pending, topic)
			return false
		}
	}
	first := now
	if prev, ok := q.pending[topic]; ok {
		first = prev.First
	}
	// OFF takes priority over pending light settings; don't restore old
	// brightness/scene values after intentionally switching the lamp off.
	if isPowerOff(topic, value) {
		for key := range q.pending {
			if key != topic && (strings.HasSuffix(key, "/brightness/set") || strings.HasSuffix(key, "/color/set") || strings.HasSuffix(key, "/temperature/set") || strings.HasSuffix(key, "/effect/set") || strings.HasSuffix(key, "/ha/set")) {
				delete(q.pending, key)
			}
		}
	}
	q.pending[topic] = queuedCommand{Topic: topic, Payload: value, First: first, Last: now}
	return true
}

// nextReady chooses the oldest actionable pending change, but never dispatches
// more than one Bluetooth operation at a time. Long drags get periodic updates.
func (q *commandQueue) nextReady(now time.Time) (queuedCommand, bool) {
	if q.busy() {
		return queuedCommand{}, false
	}
	var pick queuedCommand
	found := false
	for _, cmd := range q.pending {
		// Immediate power commands should bypass slider debounce and take
		// priority over an old slider update.
		delay := q.debounce
		priority := isPowerTopic(cmd.Topic, cmd.Payload)
		if priority {
			delay = 0
		}
		if now.Sub(cmd.Last) < delay && now.Sub(cmd.First) < q.maxWait {
			continue
		}
		selectedIsPower := isPowerTopic(pick.Topic, pick.Payload)
		if !found || (priority && !selectedIsPower) || (priority == selectedIsPower && cmd.First.Before(pick.First)) {
			pick = cmd
			found = true
		}
	}
	if found {
		delete(q.pending, pick.Topic)
		q.active = &pick
	}
	return pick, found
}

func (q *commandQueue) complete(job queuedCommand, err error, now time.Time) {
	if q.active != nil && q.active.Topic == job.Topic && q.active.Payload == job.Payload {
		q.active = nil
	}
	if err == nil {
		q.recent[job.Topic] = recentCommand{Payload: job.Payload, When: now}
	}
}
