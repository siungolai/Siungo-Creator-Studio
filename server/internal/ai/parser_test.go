package ai

import (
	"strings"
	"testing"
)

func TestParseGenerateOutputOK(t *testing.T) {
	// 模拟 markdown 围栏包裹（Go 字符串不能含反引号，用拼接）
	raw := "好的，以下是生成内容：\n```json\n" +
		`{"titles":["标题一","标题二"],"script":"开头钩子…主体…CTA","voiceover":"口播稿","tags":["AI","效率"],"cover_copy":"封面文案"}` +
		"\n```"
	out, err := ParseGenerateOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out.Titles) != 2 || out.Titles[0] != "标题一" {
		t.Fatalf("titles wrong: %+v", out.Titles)
	}
	if out.Script != "开头钩子…主体…CTA" {
		t.Fatalf("script wrong: %q", out.Script)
	}
	if out.Voiceover != "口播稿" || out.CoverCopy != "封面文案" {
		t.Fatalf("voiceover/cover wrong: %+v", out)
	}
	if len(out.Tags) != 2 {
		t.Fatalf("tags wrong: %+v", out.Tags)
	}
}

func TestParseGenerateOutputPlainJSON(t *testing.T) {
	// 无 markdown 围栏的纯 JSON
	raw := `{"titles":["t"],"script":"s","voiceover":"v","tags":[],"cover_copy":"c"}`
	out, err := ParseGenerateOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Script != "s" || len(out.Titles) != 1 {
		t.Fatalf("wrong: %+v", out)
	}
}

func TestParseGenerateOutputMissingFieldsTolerated(t *testing.T) {
	// 字段缺失容忍（零值/空切片）
	out, err := ParseGenerateOutput(`{"script":"只有脚本"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Script != "只有脚本" || out.Titles == nil || out.Tags == nil {
		t.Fatalf("wrong: %+v", out)
	}
}

func TestParseGenerateOutputMissingScript(t *testing.T) {
	// 关键字段 script 缺失 → 失败（触发重试）
	if _, err := ParseGenerateOutput(`{"titles":["t"]}`); err == nil {
		t.Fatal("want error for missing script")
	}
}

func TestParseGenerateOutputNotJSON(t *testing.T) {
	cases := []string{
		"完全不是 JSON 的文本",
		"",
		"```json\n{broken",
		"标题一\n标题二（无任何 JSON）",
	}
	for _, c := range cases {
		if _, err := ParseGenerateOutput(c); err == nil {
			t.Fatalf("want error for %q", c)
		}
	}
}

func TestExtractJSONNested(t *testing.T) {
	// 嵌套花括号也正确截取（首个 { 至末个 }）
	raw := "前缀说明 {\"a\":{\"b\":1}} 后缀"
	got := extractJSON(raw)
	if got != `{"a":{"b":1}}` {
		t.Fatalf("extract = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	long := strings.Repeat("字", 500)
	got := truncate(long)
	if len([]rune(got)) != summaryLen+1 { // +1 省略号
		t.Fatalf("truncate len = %d", len([]rune(got)))
	}
	short := "短"
	if truncate(short) != short {
		t.Fatal("short should pass through")
	}
}
