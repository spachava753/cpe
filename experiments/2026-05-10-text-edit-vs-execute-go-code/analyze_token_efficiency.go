package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	textEditName      = "text_edit"
	executeGoCodeName = "execute_go_code_edits"
)

type configFile struct {
	Models []modelConfig `yaml:"models"`
}

type modelConfig struct {
	InputCostPerMillion  float64 `yaml:"input_cost_per_million"`
	OutputCostPerMillion float64 `yaml:"output_cost_per_million"`
}

type trajectory struct {
	Steps        []step             `json:"steps"`
	FinalMetrics map[string]float64 `json:"final_metrics"`
}

type step struct {
	Timestamp string `json:"timestamp"`
}

type runRecord struct {
	Metrics         map[string]float64
	Steps           int
	DurationSeconds float64
}

type analysis struct {
	ExperimentDir               string        `json:"experiment_dir"`
	PairedValidCount            int           `json:"paired_valid_count"`
	MissingInTextEdit           []string      `json:"missing_in_text_edit"`
	MissingInExecuteGoCodeEdits []string      `json:"missing_in_execute_go_code_edits"`
	InvalidPairs                []string      `json:"invalid_pairs"`
	InputCostPerMillion         float64       `json:"input_cost_per_million"`
	OutputCostPerMillion        float64       `json:"output_cost_per_million"`
	Rows                        []analysisRow `json:"rows"`
}

type analysisRow struct {
	Metric                     string  `json:"metric"`
	TextEdit                   float64 `json:"text_edit"`
	ExecuteGoCodeEdits         float64 `json:"execute_go_code_edits"`
	Unit                       string  `json:"unit"`
	DiffTextMinusExecuteGoCode float64 `json:"diff_text_edit_minus_execute_go_code_edits"`
	SavingsWithTextEditPercent float64 `json:"savings_with_text_edit_percent"`
}

type totals struct {
	TextPrompt          int64
	ExecPrompt          int64
	TextCompletion      int64
	ExecCompletion      int64
	TextCost            float64
	ExecCost            float64
	TextDurationSeconds float64
	ExecDurationSeconds float64
	Count               int
}

func main() {
	jsonOutput := flag.Bool("json", false, "print JSON instead of the Markdown table")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: go run ./experiments/2026-05-10-text-edit-vs-execute-go-code [--json] EXPERIMENT_DIR\n")
		os.Exit(2)
	}

	result, err := analyze(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "analyze token efficiency: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "write JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	printTable(result.Rows)
}

func analyze(experimentDir string) (analysis, error) {
	inputCost, outputCost, err := readCosts(experimentDir)
	if err != nil {
		return analysis{}, err
	}

	textRuns, err := readRuns(filepath.Join(experimentDir, "trajectories", textEditName))
	if err != nil {
		return analysis{}, err
	}
	execRuns, err := readRuns(filepath.Join(experimentDir, "trajectories", executeGoCodeName))
	if err != nil {
		return analysis{}, err
	}

	result := analysis{
		ExperimentDir:               experimentDir,
		MissingInTextEdit:           missing(execRuns, textRuns),
		MissingInExecuteGoCodeEdits: missing(textRuns, execRuns),
		InvalidPairs:                []string{},
		InputCostPerMillion:         inputCost,
		OutputCostPerMillion:        outputCost,
	}

	var sum totals
	for name, textRun := range textRuns {
		execRun, ok := execRuns[name]
		if !ok {
			continue
		}
		if !valid(textRun) || !valid(execRun) {
			result.InvalidPairs = append(result.InvalidPairs, name)
			continue
		}
		result.PairedValidCount++
		sum.Count++
		sum.TextPrompt += metric(textRun, "total_prompt_tokens")
		sum.ExecPrompt += metric(execRun, "total_prompt_tokens")
		sum.TextCompletion += metric(textRun, "total_completion_tokens")
		sum.ExecCompletion += metric(execRun, "total_completion_tokens")
		sum.TextDurationSeconds += textRun.DurationSeconds
		sum.ExecDurationSeconds += execRun.DurationSeconds
	}
	sort.Strings(result.InvalidPairs)

	sum.TextCost = cost(sum.TextPrompt, sum.TextCompletion, inputCost, outputCost)
	sum.ExecCost = cost(sum.ExecPrompt, sum.ExecCompletion, inputCost, outputCost)
	result.Rows = rows(sum)
	return result, nil
}

func readCosts(experimentDir string) (float64, float64, error) {
	textModel, err := readFirstModel(filepath.Join(experimentDir, "configs", textEditName, "cpe.yaml"))
	if err != nil {
		return 0, 0, err
	}
	execModel, err := readFirstModel(filepath.Join(experimentDir, "configs", executeGoCodeName, "cpe.yaml"))
	if err != nil {
		return 0, 0, err
	}
	if textModel.InputCostPerMillion != execModel.InputCostPerMillion || textModel.OutputCostPerMillion != execModel.OutputCostPerMillion {
		return 0, 0, fmt.Errorf("config model costs differ")
	}
	return textModel.InputCostPerMillion, textModel.OutputCostPerMillion, nil
}

func readFirstModel(path string) (modelConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return modelConfig{}, err
	}
	var cfg configFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return modelConfig{}, err
	}
	if len(cfg.Models) == 0 {
		return modelConfig{}, fmt.Errorf("%s has no models", path)
	}
	return cfg.Models[0], nil
}

func readRuns(dir string) (map[string]runRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	runs := map[string]runRecord{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var traj trajectory
		if err := json.Unmarshal(data, &traj); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		durationSeconds, err := durationSeconds(traj.Steps)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		runs[entry.Name()] = runRecord{Metrics: traj.FinalMetrics, Steps: len(traj.Steps), DurationSeconds: durationSeconds}
	}
	return runs, nil
}

func rows(sum totals) []analysisRow {
	textTotal := sum.TextPrompt + sum.TextCompletion
	execTotal := sum.ExecPrompt + sum.ExecCompletion
	return []analysisRow{
		tokenRow("Prompt tokens", sum.TextPrompt, sum.ExecPrompt),
		tokenRow("Completion tokens", sum.TextCompletion, sum.ExecCompletion),
		tokenRow("Prompt + completion", textTotal, execTotal),
		costRow("Nominal cost using config rates", sum.TextCost, sum.ExecCost),
		durationRow("Average duration", sum.TextDurationSeconds/float64(sum.Count), sum.ExecDurationSeconds/float64(sum.Count)),
	}
}

func tokenRow(name string, textValue, execValue int64) analysisRow {
	return analysisRow{
		Metric:                     name,
		TextEdit:                   float64(textValue),
		ExecuteGoCodeEdits:         float64(execValue),
		Unit:                       "tokens",
		DiffTextMinusExecuteGoCode: float64(textValue - execValue),
		SavingsWithTextEditPercent: savings(float64(textValue), float64(execValue)),
	}
}

func costRow(name string, textValue, execValue float64) analysisRow {
	return analysisRow{
		Metric:                     name,
		TextEdit:                   textValue,
		ExecuteGoCodeEdits:         execValue,
		Unit:                       "usd",
		DiffTextMinusExecuteGoCode: textValue - execValue,
		SavingsWithTextEditPercent: savings(textValue, execValue),
	}
}

func durationRow(name string, textValue, execValue float64) analysisRow {
	return analysisRow{
		Metric:                     name,
		TextEdit:                   textValue,
		ExecuteGoCodeEdits:         execValue,
		Unit:                       "seconds",
		DiffTextMinusExecuteGoCode: textValue - execValue,
		SavingsWithTextEditPercent: savings(textValue, execValue),
	}
}

func printTable(rows []analysisRow) {
	fmt.Println("| Metric | `text_edit` | `execute_go_code_edits` | Savings with `text_edit` |")
	fmt.Println("|---|---:|---:|---:|")
	for _, row := range rows {
		fmt.Printf("| %s | `%s` | `%s` | `%s` |\n", row.Metric, display(row.TextEdit, row.Unit), display(row.ExecuteGoCodeEdits, row.Unit), fmt.Sprintf("%.2f%%", row.SavingsWithTextEditPercent))
	}
}

func display(value float64, unit string) string {
	switch unit {
	case "usd":
		return fmt.Sprintf("$%.4f", value)
	case "seconds":
		return fmt.Sprintf("%.1fs", value)
	default:
		return comma(int64(math.Round(value)))
	}
}

func missing(source, target map[string]runRecord) []string {
	out := []string{}
	for name := range source {
		if _, ok := target[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func valid(run runRecord) bool {
	return run.Steps > 1 && run.Metrics["total_prompt_tokens"] > 0 && run.Metrics["total_completion_tokens"] > 0
}

func durationSeconds(steps []step) (float64, error) {
	if len(steps) < 2 {
		return 0, nil
	}
	start, err := parseTimestamp(steps[0].Timestamp)
	if err != nil {
		return 0, err
	}
	end, err := parseTimestamp(steps[len(steps)-1].Timestamp)
	if err != nil {
		return 0, err
	}
	return end.Sub(start).Seconds(), nil
}

func parseTimestamp(value string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02T15:04:05", value); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, value)
}

func metric(run runRecord, name string) int64 {
	return int64(math.Round(run.Metrics[name]))
}

func cost(promptTokens, completionTokens int64, inputCostPerMillion, outputCostPerMillion float64) float64 {
	return float64(promptTokens)/1_000_000*inputCostPerMillion + float64(completionTokens)/1_000_000*outputCostPerMillion
}

func savings(textValue, execValue float64) float64 {
	return 100 * (execValue - textValue) / execValue
}

func comma(value int64) string {
	digits := strconv.FormatInt(value, 10)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}
