package billing

import "time"

var DunningSchedule = []time.Duration{24 * time.Hour, 72 * time.Hour, 7 * 24 * time.Hour}

func NextRetry(failedAttempts int, firstFailure time.Time) (time.Time, bool) {
	if failedAttempts <= 0 || failedAttempts > len(DunningSchedule) {
		return time.Time{}, false
	}
	return firstFailure.Add(DunningSchedule[failedAttempts-1]), true
}
