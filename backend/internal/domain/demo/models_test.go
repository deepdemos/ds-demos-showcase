package demo

import (
	"fmt"
	"strings"
	"testing"
)

// TestSummarizeUnknownCapsTheList 钉住「400 的正文不随请求体线性变长」。
//
// 一次点名所有没认出来的指称是为了让作者改一次就够；但请求里能塞多少个名字，
// 正文就会跟着长多少。超过上限时只点名前 maxUnknownInMessage 个，再报一个总数——
// 作者仍然知道「还有哪些没认出来」，而不是收到一份原样回显的输入。
func TestSummarizeUnknownCapsTheList(t *testing.T) {
	short := []string{"甲", "乙"}
	if got := summarizeUnknown(short); got != "甲、乙" {
		t.Fatalf("两个名字 = %q，想要 %q", got, "甲、乙")
	}

	exact := make([]string, maxUnknownInMessage)
	for i := range exact {
		exact[i] = fmt.Sprintf("n%02d", i)
	}
	if got := summarizeUnknown(exact); !strings.HasPrefix(got, "n00、n01") || strings.Contains(got, "等") {
		t.Fatalf("恰好 %d 个时应当全部点名、不带「等」：%q", maxUnknownInMessage, got)
	}

	many := append(append([]string{}, exact...), "多出来的甲", "多出来的乙")
	got := summarizeUnknown(many)
	if !strings.Contains(got, "等 22 个") {
		t.Fatalf("超限时应当报总数：%q", got)
	}
	if strings.Contains(got, "多出来的甲") {
		t.Fatalf("超限时不该回显第 %d 个之后的指称：%q", maxUnknownInMessage+1, got)
	}
	if !strings.HasPrefix(got, "n00、n01") {
		t.Fatalf("超限时仍应点名前几个：%q", got)
	}
}
