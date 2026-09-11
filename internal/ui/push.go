package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// pushAttempts caps the automatic attempts of a docker push. A registry that
// turns a push away — a timeout, or ACR's firewall refusing the client for a
// moment — usually takes the same push shortly after, and the layers already
// uploaded are not sent again. Past the cap the failure is no longer a hiccup,
// and what to do about it is asked instead of tried.
const pushAttempts = 3

// pushRetryDelay spaces the attempts out, so a registry that has just refused
// the client is not asked again within the same second.
const pushRetryDelay = 5 * time.Second

// pushLabel is the spinner label of a push, naming the attempt once it is not
// the first.
func pushLabel(label string, attempt int) string {
	if note := pushNote(attempt); note != "" {
		return label + " · " + note
	}
	return label
}

// pushNote places a push among its attempts, once there has been more than one.
func pushNote(attempt int) string {
	if attempt <= 1 {
		return ""
	}
	return fmt.Sprintf("tentativo %d/%d", attempt, pushAttempts)
}

// logPushRetry is an attempt that failed and is about to be repeated. Underneath
// goes the registry's own reason — the last line docker printed — rather than
// the exit status, which reads the same whatever went wrong.
func logPushRetry(attempt int, err error, output []byte, elapsed string) []string {
	lines := []string{logWarnStep("Push", fmt.Sprintf("tentativo %d/%d non riuscito", attempt, pushAttempts), elapsed)}
	reason := lastOutputLine(output)
	if reason == "" && err != nil {
		reason = err.Error()
	}
	if reason != "" {
		lines = append(lines, logDetail(reason))
	}
	return lines
}

// logPushFailure is the last attempt failed: the whole output this time, since
// nothing is going to run the push again on its own.
func logPushFailure(attempt int, err error, output []byte, elapsed string) []string {
	lines := []string{logFail("Push", pushNote(attempt), elapsed)}
	if err != nil {
		lines = append(lines, logDetail(err.Error()))
	}
	return appendOutput(lines, output)
}

// lastOutputLine is the last non-empty line of a command's output. For docker
// push that is the registry's answer, after the list of layers.
func lastOutputLine(out []byte) string {
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// pushErrorItems are the ways out of a push that failed every attempt. Skipping
// is offered only when something else is left to deploy: otherwise it would be
// cancelling under another name.
func pushErrorItems(service, oldTag string, othersLeft bool, cancelDesc string) []Item {
	items := []Item{{Value: "retry", Label: "Riprova", Desc: "riesegue docker push"}}
	if othersLeft {
		items = append(items, Item{Value: "skip", Label: "Salta",
			Desc: "prosegue senza " + service + ": sul cluster resta " + oldTag})
	}
	return append(items, Item{Value: "cancel", Label: "Annulla", Desc: cancelDesc})
}

// logPushSkipped records a service left out after its push failed. The summary
// lists only what was deployed, so the log is where the gap is stated.
func logPushSkipped(service, oldTag string) []string {
	return logWarn(service + " saltato: sul cluster resta " + oldTag)
}

// In --test-ui the first push of the last service is turned away once, the way
// a registry does in a real run: without it the retry would be a path no
// simulated run ever shows. The address is from the documentation range.
var errSimulatedPush = errors.New("docker push fallito: exit status 1")

const (
	simulatedACRRefusal = "denied: client with IP '203.0.113.10' is not allowed access, refer https://aka.ms/acr/firewall to grant access"
	simulatedECRTimeout = "net/http: TLS handshake timeout"
)

// refusesSimulatedPush reports whether a --test-ui push is the one turned away:
// the first attempt for the last service, the case the retry was written for.
func refusesSimulatedPush(attempt, idx, total int) bool {
	return attempt == 1 && idx == total-1
}
