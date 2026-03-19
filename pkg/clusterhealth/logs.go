package clusterhealth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	logTypeCurrent  = "current"
	logTypePrevious = "previous"

	logFetchTimeout     = 10 * time.Second
	logFetchConcurrency = 5
	maxLogBytes         = 10 * 1024 * 1024 // 10 MiB safety cap per container
)

// logConfig is the internal bundle passed to section runners that need log capture.
type logConfig struct {
	clientset *kubernetes.Clientset
	tailLines int64
}

// Known problematic waiting reasons that warrant log capture even without restarts.
var problematicWaitingReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"InvalidImageName":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
}

var redactionPatterns = []*regexp.Regexp{
	// Header-style: Authorization: Bearer <token>
	regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`),
	// key=value and key: value (unquoted values)
	regexp.MustCompile(`(?i)(password[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(token[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(secret[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(api[_-]?key[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(access[_-]?key[=:\s]+)[^\s&"']+`),
	// Quoted values: key="value", key='value', key: "value", key: 'value' (logfmt, YAML, config)
	regexp.MustCompile(`(?i)(password[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(password[=:]\s*')[^']*(')`),
	regexp.MustCompile(`(?i)(token[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(token[=:]\s*')[^']*(')`),
	regexp.MustCompile(`(?i)(secret[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(secret[=:]\s*')[^']*(')`),
	regexp.MustCompile(`(?i)(api[_-]?key[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(api[_-]?key[=:]\s*')[^']*(')`),
	regexp.MustCompile(`(?i)(access[_-]?key[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(access[_-]?key[=:]\s*')[^']*(')`),
	regexp.MustCompile(`(?i)(authorization[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(authorization[=:]\s*')[^']*(')`),
	regexp.MustCompile(`(?i)(credential[s]?[=:]\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)(credential[s]?[=:]\s*')[^']*(')`),
	// JSON-structured: {"password":"value"} or {"token": "value"}
	regexp.MustCompile(`(?i)("password"\s*:\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)("token"\s*:\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)("secret"\s*:\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)("api[_-]?key"\s*:\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)("access[_-]?key"\s*:\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)("authorization"\s*:\s*")[^"]*(")`),
	regexp.MustCompile(`(?i)("credential[s]?"\s*:\s*")[^"]*(")`),
}

// determineLogType decides whether to fetch logs for a container and whether
// to fetch current or previous logs. Returns "" if no logs are needed.
// podPhase is the owning pod's phase; containers from Succeeded pods are
// always skipped because they terminated normally.
func determineLogType(c ContainerInfo, podPhase string) string {
	if podPhase == string(corev1.PodSucceeded) {
		return ""
	}

	// Waiting after a restart → the interesting logs are in the previous instance.
	if c.Waiting != "" && c.RestartCount > 0 {
		return logTypePrevious
	}

	if c.Terminated != "" {
		return logTypeCurrent
	}

	if !c.Ready && c.RestartCount > 0 {
		return logTypeCurrent
	}

	// Known-bad waiting states even without restarts (e.g. first ImagePullBackOff).
	if c.Waiting != "" {
		reason := strings.Fields(c.Waiting)[0]
		if problematicWaitingReasons[reason] {
			return logTypeCurrent
		}
	}

	return ""
}

// redactSensitiveInfo removes common secrets and tokens from log content.
func redactSensitiveInfo(logContent string) string {
	result := logContent
	for _, p := range redactionPatterns {
		result = p.ReplaceAllString(result, "${1}[REDACTED]${2}")
	}
	return result
}

// fetchContainerLogs retrieves the tail of a container's logs via the Kubernetes API.
func fetchContainerLogs(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string, tailLines int64, previous bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()

	opts := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
		Previous:  previous,
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("open log stream: %w", err)
	}
	defer stream.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, io.LimitReader(stream, maxLogBytes)); err != nil {
		return "", fmt.Errorf("read log stream: %w", err)
	}
	if int64(buf.Len()) >= maxLogBytes {
		return buf.String() + "\n[truncated: log output exceeded 10 MiB safety limit]", nil
	}

	return buf.String(), nil
}

type logRequest struct {
	podIndex       int
	containerIndex int
	namespace      string
	podName        string
	containerName  string
	previous       bool
}

type logResult struct {
	podIndex       int
	containerIndex int
	logs           string
	logError       string
	previous       bool
}

// buildLogRequests returns one logRequest per container that needs log
// capture, consulting determineLogType to skip healthy containers.
func buildLogRequests(pods []PodInfo) []logRequest {
	var requests []logRequest
	for pi := range pods {
		for ci := range pods[pi].Containers {
			lt := determineLogType(pods[pi].Containers[ci], pods[pi].Phase)
			if lt == "" {
				continue
			}
			requests = append(requests, logRequest{
				podIndex:       pi,
				containerIndex: ci,
				namespace:      pods[pi].Namespace,
				podName:        pods[pi].Name,
				containerName:  pods[pi].Containers[ci].Name,
				previous:       lt == logTypePrevious,
			})
		}
	}
	return requests
}

// captureLogsForPods fetches logs for all problematic containers across the
// given pods. It modifies the ContainerInfo entries in-place. Safe to call
// with a nil clientset (no-op).
func captureLogsForPods(ctx context.Context, clientset *kubernetes.Clientset, tailLines int64, pods []PodInfo) {
	if clientset == nil || tailLines < 0 {
		return
	}
	if tailLines == 0 {
		tailLines = defaultLogTailLines
	}

	requests := buildLogRequests(pods)
	if len(requests) == 0 {
		return
	}

	results := make([]logResult, len(requests))
	var wg sync.WaitGroup
	sem := make(chan struct{}, logFetchConcurrency)

	for i, req := range requests {
		wg.Add(1)
		go func(idx int, r logRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			logs, err := fetchContainerLogs(ctx, clientset, r.namespace, r.podName, r.containerName, tailLines, r.previous)
			res := logResult{
				podIndex:       r.podIndex,
				containerIndex: r.containerIndex,
				previous:       r.previous,
			}
			if err != nil {
				res.logError = err.Error()
			} else if logs != "" {
				res.logs = redactSensitiveInfo(strings.TrimSpace(logs))
			}
			results[idx] = res
		}(i, req)
	}
	wg.Wait()

	for _, res := range results {
		c := &pods[res.podIndex].Containers[res.containerIndex]
		c.Logs = res.logs
		c.LogError = res.logError
		c.LogPrevious = res.previous
	}
}
