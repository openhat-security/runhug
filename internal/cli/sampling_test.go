package cli

import (
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hparams"
)

func TestChooseSamplingYesRecommended(t *testing.T) {
	temp := 0.6
	rec := hparams.Sampling{Temperature: &temp, Source: "qwen3"}
	got, err := chooseSampling(rec, "recommended", nil, true, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Temperature == nil || *got.Temperature != 0.6 {
		t.Fatalf("%+v", got)
	}
}

func TestChooseSamplingNone(t *testing.T) {
	temp := 0.6
	rec := hparams.Sampling{Temperature: &temp}
	got, err := chooseSampling(rec, "none", nil, true, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestChooseSamplingSetOverride(t *testing.T) {
	temp := 0.6
	rec := hparams.Sampling{Temperature: &temp, Source: "qwen3"}
	got, err := chooseSampling(rec, "recommended", []string{"temp=0.2"}, true, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Temperature == nil || *got.Temperature != 0.2 {
		t.Fatalf("%+v", got)
	}
	if got.Source != "custom" {
		t.Fatalf("source %s", got.Source)
	}
}

func TestChooseSamplingDryJSON(t *testing.T) {
	temp := 0.7
	rec := hparams.Sampling{Temperature: &temp}
	got, err := chooseSampling(rec, "", nil, false, true, true, false)
	if err != nil || got == nil {
		t.Fatalf("%v %+v", err, got)
	}
	if !strings.Contains(got.FormatBlock(), "temp=0.70") {
		t.Fatalf("%s", got.FormatBlock())
	}
}
