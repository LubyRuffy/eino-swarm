package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// execSleepBiasMin is the shortest pause that counts as a remaining-time
// wait. Sub-second gaps between commands stay as written. At or above this,
// exec applies the same short bias as schedule_wake: about a third of the
// asked duration, because extra checks are cheap and a late poll looks late.
const execSleepBiasMin = 5 * time.Second

const execSleepBiasInfo = " A sleep that polls remaining time uses about a third of that estimate; extra checks are fine. Do not pad."

var (
	unixSleepRe         = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_-])sleep\s+(\d+(?:\.\d+)?)([smhd])?\b`)
	startSleepSecondsRe = regexp.MustCompile(`(?i)\bstart-sleep\s+-(?:seconds?|s)\s+(\d+(?:\.\d+)?)\b`)
	startSleepMilliRe   = regexp.MustCompile(`(?i)\bstart-sleep\s+-milli(?:seconds?)?\s+(\d+(?:\.\d+)?)\b`)
	startSleepBareRe    = regexp.MustCompile(`(?i)\bstart-sleep\s+(\d+(?:\.\d+)?)\b`)
)

type execSleepBias struct {
	inner tool.InvokableTool
}

func wrapExecSleepBias(inner tool.BaseTool) (tool.BaseTool, error) {
	inv, ok := inner.(tool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("tools: exec must be invokable")
	}
	return &execSleepBias{inner: inv}, nil
}

func (g *execSleepBias) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := g.inner.Info(ctx)
	if err != nil || info == nil {
		return info, err
	}
	out := *info
	if !strings.Contains(out.Desc, "about a third of that estimate") {
		out.Desc = strings.TrimSpace(out.Desc) + execSleepBiasInfo
	}
	return &out, nil
}

func (g *execSleepBias) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	return g.inner.InvokableRun(ctx, biasExecSleepArgs(args), opts...)
}

func biasExecSleepArgs(args string) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return args
	}
	var command string
	if err := json.Unmarshal(raw["command"], &command); err != nil {
		return args
	}
	next := biasExecSleepCommand(command)
	if next == command {
		return args
	}
	// Marshal of a string / map[string]json.RawMessage does not fail;
	// ignoring the error keeps this from looking like a refusal path.
	b, _ := json.Marshal(next)
	raw["command"] = b
	out, _ := json.Marshal(raw)
	return string(out)
}

func biasExecSleepCommand(command string) string {
	command = mapSleepMatches(command, unixSleepRe, 2, biasUnixSleep)
	command = mapSleepMatches(command, startSleepSecondsRe, 1, biasStartSleepSeconds)
	command = mapSleepMatches(command, startSleepMilliRe, 1, biasStartSleepMillis)
	if !startSleepSecondsRe.MatchString(command) && !startSleepMilliRe.MatchString(command) {
		command = mapSleepMatches(command, startSleepBareRe, 1, biasStartSleepSeconds)
	}
	return command
}

func biasUnixSleep(num, unit string) (string, bool) {
	d := parseSleepAmount(num, unit)
	if d < execSleepBiasMin {
		return "", false
	}
	return formatSleepSeconds(d / 3), true
}

func biasStartSleepSeconds(num, _ string) (string, bool) {
	d := parseSleepAmount(num, "s")
	if d < execSleepBiasMin {
		return "", false
	}
	return formatSleepSeconds(d / 3), true
}

func biasStartSleepMillis(num, _ string) (string, bool) {
	d := parseSleepMillis(num)
	if d < execSleepBiasMin {
		return "", false
	}
	return strconv.FormatInt((d / 3).Milliseconds(), 10), true
}

func mapSleepMatches(s string, re *regexp.Regexp, groups int, fn func(num, unit string) (string, bool)) string {
	all := re.FindAllStringSubmatchIndex(s, -1)
	if len(all) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range all {
		num := s[loc[2]:loc[3]]
		unit := ""
		end := loc[3]
		if groups >= 2 && len(loc) >= 6 && loc[4] >= 0 {
			unit = s[loc[4]:loc[5]]
			end = loc[5]
		}
		b.WriteString(s[last:loc[2]])
		if repl, ok := fn(num, unit); ok {
			b.WriteString(repl)
		} else {
			b.WriteString(s[loc[2]:end])
		}
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func formatSleepSeconds(d time.Duration) string {
	sec := d.Seconds()
	if sec == float64(int64(sec)) {
		return strconv.FormatInt(int64(sec), 10)
	}
	s := strconv.FormatFloat(sec, 'f', 3, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func execSleepWait(command string) time.Duration {
	command = strings.TrimSpace(command)
	if command == "" {
		return 0
	}
	var max time.Duration
	for _, m := range unixSleepRe.FindAllStringSubmatch(command, -1) {
		if d := parseSleepAmount(m[1], m[2]); d > max {
			max = d
		}
	}
	for _, m := range startSleepSecondsRe.FindAllStringSubmatch(command, -1) {
		if d := parseSleepAmount(m[1], "s"); d > max {
			max = d
		}
	}
	for _, m := range startSleepMilliRe.FindAllStringSubmatch(command, -1) {
		if d := parseSleepMillis(m[1]); d > max {
			max = d
		}
	}
	if !startSleepSecondsRe.MatchString(command) && !startSleepMilliRe.MatchString(command) {
		for _, m := range startSleepBareRe.FindAllStringSubmatch(command, -1) {
			if d := parseSleepAmount(m[1], "s"); d > max {
				max = d
			}
		}
	}
	return max
}

func parseSleepMillis(n string) time.Duration {
	v, err := strconv.ParseFloat(n, 64)
	if err != nil || v < 0 {
		return 0
	}
	return time.Duration(v * float64(time.Millisecond))
}

func parseSleepAmount(n, unit string) time.Duration {
	v, err := strconv.ParseFloat(n, 64)
	if err != nil || v < 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "s":
		return time.Duration(v * float64(time.Second))
	case "m":
		return time.Duration(v * float64(time.Minute))
	case "h":
		return time.Duration(v * float64(time.Hour))
	case "d":
		return time.Duration(v * float64(24*time.Hour))
	default:
		return 0
	}
}
