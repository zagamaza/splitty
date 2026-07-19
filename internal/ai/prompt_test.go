package ai

import (
	"strings"
	"testing"
)

func TestBuildPrompt_RequesterRule(t *testing.T) {
	p := buildPrompt(ParseInput{RequesterId: 101})
	if !strings.Contains(p, "id=101") || !strings.Contains(p, "«я»") {
		t.Fatalf("нет правила про отправителя: %s", p)
	}
}

func TestBuildPrompt_NoRequesterNoRule(t *testing.T) {
	p := buildPrompt(ParseInput{})
	if strings.Contains(p, "Запрос отправил") {
		t.Fatalf("правило про отправителя не должно добавляться без id")
	}
}
