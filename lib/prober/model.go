package prober

import (
	"fmt"
	"sort"
	"strings"
)

// Target is one demanded departure slot. Needed is the maximum number of holds
// a single Job may acquire before it stops probing this slot.
type Target struct {
	Direction string `json:"dir"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Needed    int    `json:"needed"`
}

func (t Target) Key() string {
	return fmt.Sprintf("%s:%s:%s", t.Direction, t.Date, t.Time)
}

type SlotTally struct {
	Slot        string `json:"slot"`
	Polls       int64  `json:"polls"`
	Holds       int64  `json:"holds"`
	SoldOut     int64  `json:"soldOut"`
	Stale       int64  `json:"stale"`
	Errors      int64  `json:"errors"`
	RateLimited int64  `json:"rateLimited"`
	SessionDead int64  `json:"sessionDead"`
	Skipped     int64  `json:"skipped"`
}

type JobTally struct {
	Epoch int64       `json:"epoch"`
	Job   string      `json:"job"`
	Slots []SlotTally `json:"slots"`
	Total SlotTally   `json:"total"`
}

func TargetsFromCount(counts map[string]map[string]map[string]int) []Target {
	targets := make([]Target, 0)
	for direction, dates := range counts {
		for date, times := range dates {
			for departure, needed := range times {
				if needed > 0 {
					targets = append(targets, Target{Direction: direction, Date: date, Time: departure, Needed: needed})
				}
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Date != targets[j].Date {
			return targets[i].Date < targets[j].Date
		}
		if targets[i].Direction != targets[j].Direction {
			return targets[i].Direction < targets[j].Direction
		}
		return targets[i].Time < targets[j].Time
	})
	return targets
}

func ShardTargets(targets []Target, size int) [][]Target {
	if len(targets) == 0 {
		return nil
	}
	if size <= 0 || size >= len(targets) {
		return [][]Target{targets}
	}
	shards := make([][]Target, 0, (len(targets)+size-1)/size)
	for start := 0; start < len(targets); start += size {
		end := start + size
		if end > len(targets) {
			end = len(targets)
		}
		shards = append(shards, targets[start:end])
	}
	return shards
}

func Matches(messages, patterns []string) bool {
	for _, message := range messages {
		message = strings.ToLower(message)
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(strings.ToLower(pattern))
			if pattern != "" && strings.Contains(message, pattern) {
				return true
			}
		}
	}
	return false
}

func SumTallies(epoch int64, job string, slots []SlotTally) JobTally {
	total := SlotTally{Slot: "total"}
	for _, slot := range slots {
		total.Polls += slot.Polls
		total.Holds += slot.Holds
		total.SoldOut += slot.SoldOut
		total.Stale += slot.Stale
		total.Errors += slot.Errors
		total.RateLimited += slot.RateLimited
		total.SessionDead += slot.SessionDead
		total.Skipped += slot.Skipped
	}
	return JobTally{Epoch: epoch, Job: job, Slots: slots, Total: total}
}
