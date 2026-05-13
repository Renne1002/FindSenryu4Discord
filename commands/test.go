package commands

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/0x307e/go-haiku"
	"github.com/bwmarrin/discordgo"
	"github.com/ikawaha/kagome-dict/dict"
	"github.com/ikawaha/kagome-dict/uni"
	"github.com/ikawaha/kagome/v2/tokenizer"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/msgtmpl"
	"github.com/u16-io/FindSenryu4Discord/service"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

// HandleTestCommand handles the /test slash command.
func HandleTestCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("test")

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, msgtmpl.Get("test.text_required", "判定したい文章を指定してください"))
		return
	}

	input := strings.TrimSpace(options[0].StringValue())
	if input == "" {
		respondError(s, i, msgtmpl.Get("test.text_required", "判定したい文章を指定してください"))
		return
	}

	report := judgeSenryuDetailed(input)
	respondEphemeral(s, i, buildDetailedFeedback(report))
}

type senryuJudgeReport struct {
	Input              string
	Cleaned            string
	OK                 bool
	Detected           string
	Reason             string
	JapaneseRatio      float64
	Candidates         []string
	ContainsTokens     bool
	HadSpoiler         bool
	CodeBlocksStripped bool
	BannedWords        []string
}

type tokenMoraInfo struct {
	Surface   string
	Reading   string
	Strict    int
	Ambiguous int
	Units     string
	Features  []string
}

func judgeSenryuDetailed(input string) senryuJudgeReport {
	report := senryuJudgeReport{Input: input}

	report.ContainsTokens = containsDiscordTokens(input)
	if report.ContainsTokens {
		report.Reason = msgtmpl.Get("test.reason_discord_tokens", "メンション・URL・カスタム絵文字などのDiscordトークンを含んでいます")
		return report
	}

	content := input
	report.HadSpoiler = containsSpoiler(content)
	if report.HadSpoiler {
		content = stripSpoilerMarkers(content)
	}
	stripped := stripCodeBlocks(content)
	report.CodeBlocksStripped = stripped != content
	content = strings.TrimSpace(stripped)
	report.Cleaned = content

	if content == "" {
		report.Reason = msgtmpl.Get("test.reason_empty_after_cleanup", "コードブロック等を除去すると本文が空になります")
		return report
	}

	report.JapaneseRatio = japaneseCharRatio(content)
	if !isJapaneseRich(content) {
		report.Reason = msgtmpl.Get("test.reason_not_japanese_rich", "日本語の割合が少ないため判定対象外です")
		return report
	}

	report.Candidates = findHaikuSafe(content, []int{5, 7, 5})
	if len(report.Candidates) == 0 {
		report.Reason = msgtmpl.Get("test.reason_not_575", "五・七・五のリズムを検出できませんでした")
		return report
	}

	if haikuSpansNewline(content, report.Candidates[0]) {
		report.Reason = msgtmpl.Get("test.reason_spans_newline", "改行をまたぐ五・七・五は判定対象外です")
		return report
	}

	if blocked, words := service.MatchBannedWords(report.Candidates[0]); blocked {
		report.BannedWords = words
		report.Reason = msgtmpl.Format("test.reason_banned_word", "禁止ワードを含んでいます: %s", strings.Join(words, ", "))
		return report
	}

	report.OK = true
	report.Detected = report.Candidates[0]
	return report
}

func buildDetailedFeedback(report senryuJudgeReport) string {
	var b strings.Builder
	if report.OK {
		fmt.Fprintf(&b, "✅ 川柳として判定されました\n")
		fmt.Fprintf(&b, "検出句: 「%s」\n", report.Detected)
	} else {
		fmt.Fprintf(&b, "❌ 川柳として判定されませんでした\n")
		if report.Reason != "" {
			fmt.Fprintf(&b, "理由: %s\n", report.Reason)
		}
	}

	b.WriteString("\n【前処理】\n")
	if report.Cleaned == "" {
		b.WriteString("テキスト: (空)\n")
	} else {
		b.WriteString("テキスト:\n")
		fmt.Fprintf(&b, "```\n%s\n```\n", trimForDisplay(report.Cleaned, 300))
	}
	fmt.Fprintf(&b, "- Discordトークン: %s\n", yesNo(report.ContainsTokens))
	fmt.Fprintf(&b, "- スポイラー除去: %s\n", yesNo(report.HadSpoiler))
	fmt.Fprintf(&b, "- コード除去: %s\n", yesNo(report.CodeBlocksStripped))
	if report.Cleaned != "" {
		fmt.Fprintf(&b, "- 日本語比率: %.1f%% (閾値 50%%)\n", report.JapaneseRatio*100)
	}

	b.WriteString("\n【5-7-5候補】\n")
	if len(report.Candidates) == 0 {
		b.WriteString("- 候補なし\n")
		if report.Cleaned != "" {
			b.WriteString("\n【候補外の詳細診断】\n")
			b.WriteString(buildNoCandidateDiagnostics(report.Cleaned))
		}
	} else {
		maxCandidates := minInt(len(report.Candidates), 3)
		for idx := 0; idx < maxCandidates; idx++ {
			fmt.Fprintf(&b, "%d. %s\n", idx+1, report.Candidates[idx])
		}
		if len(report.Candidates) > maxCandidates {
			fmt.Fprintf(&b, "- ...他 %d 件\n", len(report.Candidates)-maxCandidates)
		}
	}

	target := report.Detected
	if target == "" && len(report.Candidates) > 0 {
		target = report.Candidates[0]
	}
	if target != "" {
		b.WriteString("\n【音節内訳】\n")
		b.WriteString(buildMoraDiagnostics(target))
	}

	return trimForDisplay(b.String(), 1900)
}

func buildMoraDiagnostics(candidate string) string {
	parts := strings.Split(candidate, " ")
	targets := []int{5, 7, 5}
	labels := []string{"上五", "中七", "下五"}

	var b strings.Builder
	for i, p := range parts {
		label := fmt.Sprintf("句%d", i+1)
		target := 0
		if i < len(labels) {
			label = labels[i]
		}
		if i < len(targets) {
			target = targets[i]
		}

		infos, err := tokenizeForMora(p)
		if err != nil {
			fmt.Fprintf(&b, "- %s: %s (解析失敗: %v)\n", label, p, err)
			continue
		}

		strict := 0
		amb := 0
		fmt.Fprintf(&b, "%s: %s\n", label, p)
		for _, info := range infos {
			strict += info.Strict
			amb += info.Ambiguous
			fmt.Fprintf(&b, "  - %s -> %s | strict=%d / min=%d\n", info.Surface, info.Reading, info.Strict, info.Strict-info.Ambiguous)
			fmt.Fprintf(&b, "    内訳: %s\n", info.Units)
		}
		if target > 0 {
			fmt.Fprintf(&b, "  合計: strict=%d / min=%d / target=%d\n", strict, strict-amb, target)
		} else {
			fmt.Fprintf(&b, "  合計: strict=%d / min=%d\n", strict, strict-amb)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func buildNoCandidateDiagnostics(cleaned string) string {
	infos, err := tokenizeForMora(cleaned)
	if err != nil {
		return fmt.Sprintf("解析失敗: %v\n", err)
	}
	if len(infos) == 0 {
		return "有効トークンがありません\n"
	}

	var b strings.Builder
	strictTotal := 0
	minTotal := 0
	nonKanaReadings := make([]string, 0)

	b.WriteString("トークン診断:\n")
	for idx, info := range infos {
		strictTotal += info.Strict
		minVal := info.Strict - info.Ambiguous
		minTotal += minVal
		pos := posSummary(info.Features)
		if !reWord.MatchString(info.Reading) {
			nonKanaReadings = append(nonKanaReadings, info.Reading)
		}
		fmt.Fprintf(&b, "%d. %s -> %s | %s | %d~%d | 累積 %d~%d\n",
			idx+1, info.Surface, info.Reading, pos, info.Strict, minVal, strictTotal, minTotal)
	}

	fmt.Fprintf(&b, "\n総量: strict=%d / min=%d (目標は17)\n", strictTotal, minTotal)

	b.WriteString("575到達の見立て:\n")
	for _, line := range estimate575Failure(infos) {
		fmt.Fprintf(&b, "- %s\n", line)
	}

	if len(nonKanaReadings) > 0 {
		fmt.Fprintf(&b, "- カナ読みに変換できない要素があります: %s\n", strings.Join(uniqueStrings(nonKanaReadings), ", "))
	}

	if strictTotal < 17 {
		fmt.Fprintf(&b, "- strict不足: あと %d 必要\n", 17-strictTotal)
	} else if minTotal > 17 {
		fmt.Fprintf(&b, "- minでも超過: %d 文字超過\n", minTotal-17)
	} else if strictTotal == 17 || (minTotal <= 17 && strictTotal >= 17) {
		b.WriteString("- 文字数上は近いですが、語の切れ目・品詞境界・読みの扱いで 5-7-5 が成立しない可能性があります\n")
		boundaryLines := diagnoseBoundaryConstraints(infos)
		if len(boundaryLines) > 0 {
			b.WriteString("- 品詞境界の詳細:\n")
			for _, line := range boundaryLines {
				fmt.Fprintf(&b, "  - %s\n", line)
			}
		}
	}

	return b.String()
}

func diagnoseBoundaryConstraints(infos []tokenMoraInfo) []string {
	targets := []int{5, 7, 5}
	lines := make([]string, 0, 4)

	startIdx := []int{0}
	endIdx := make([]int, 0, len(targets))

	acc := 0
	nextTargetIdx := 0
	nextTarget := targets[0]

	for i, info := range infos {
		acc += info.Strict
		if acc == nextTarget {
			endIdx = append(endIdx, i)
			nextTargetIdx++
			if nextTargetIdx >= len(targets) {
				break
			}
			startIdx = append(startIdx, i+1)
			nextTarget += targets[nextTargetIdx]
		}
		if acc > nextTarget {
			return lines
		}
	}

	if len(endIdx) != len(targets) {
		return lines
	}

	for part := 0; part < len(targets); part++ {
		if part >= len(startIdx) || startIdx[part] >= len(infos) {
			break
		}
		st := infos[startIdx[part]]
		if !isWordForDebug(st.Features) {
			lines = append(lines, fmt.Sprintf("句%d開始トークン '%s' は開始不可の品詞です (%s)", part+1, st.Surface, posSummary(st.Features)))
		}

		ed := infos[endIdx[part]]
		if !isEndForDebug(ed.Features) {
			lines = append(lines, fmt.Sprintf("句%d終了トークン '%s' は終了不可の品詞です (%s)", part+1, ed.Surface, posSummary(ed.Features)))
		}
	}

	return lines
}

func estimate575Failure(infos []tokenMoraInfo) []string {
	targets := []int{5, 7, 5}
	part := 0
	remain := targets[0]
	lines := make([]string, 0, 4)

	for _, info := range infos {
		if part >= len(targets) {
			lines = append(lines, "下五以降にもトークンが残っています")
			break
		}

		strict := info.Strict
		minVal := info.Strict - info.Ambiguous

		if minVal > remain {
			lines = append(lines, fmt.Sprintf("%s の時点で句%dを超過 (min=%d > 残り=%d)", info.Surface, part+1, minVal, remain))
			return lines
		}
		if strict > remain && minVal <= remain {
			lines = append(lines, fmt.Sprintf("%s は曖昧文字を含み句%d境界付近で揺れます (strict=%d, min=%d, 残り=%d)", info.Surface, part+1, strict, minVal, remain))
		}

		remain -= strict
		if remain == 0 {
			part++
			if part < len(targets) {
				remain = targets[part]
			}
		} else if remain < 0 {
			lines = append(lines, fmt.Sprintf("%s の時点で句%dを strict ベースで超過 (%d)", info.Surface, part+1, -remain))
			return lines
		}
	}

	if part < len(targets) {
		lines = append(lines, fmt.Sprintf("句%dまで到達できず終了 (残り=%d)", part+1, remain))
	} else if remain == 0 {
		lines = append(lines, "strict ベースでは 5-7-5 に到達")
	}

	return lines
}

func tokenizeForMora(text string) ([]tokenMoraInfo, error) {
	d := uni.Dict()
	t, err := tokenizer.New(d, tokenizer.OmitBosEos())
	if err != nil {
		return nil, err
	}

	normText := norm.NFC.String(width.Widen.String(text))
	normText = reIgnoreText.ReplaceAllString(normText, " ")
	tokens := t.Tokenize(normText)

	ret := make([]tokenMoraInfo, 0, len(tokens))
	for _, tok := range tokens {
		f := tok.Features()
		if isIgnoreToken(f) {
			continue
		}
		reading := tokenReading(d, tok)
		if reading == "" {
			reading = tok.Surface
		}
		units, strict, amb := moraUnitBreakdown(reading)
		ret = append(ret, tokenMoraInfo{
			Surface:   tok.Surface,
			Reading:   reading,
			Strict:    strict,
			Ambiguous: amb,
			Units:     units,
			Features:  f,
		})
	}
	return ret, nil
}

func containsFeature(c []string, s string) bool {
	for _, cc := range c {
		if cc == s {
			return true
		}
	}
	return false
}

func isEndForDebug(c []string) bool {
	if len(c) == 0 {
		return false
	}
	if c[0] == "接頭辞" {
		if containsFeature(c, "御") {
			return false
		}
		return true
	}
	if len(c) > 1 && c[1] == "非自立" {
		if c[0] == "名詞" || c[0] == "動詞" {
			return true
		}
		if containsFeature(c, "ノ") {
			return true
		}
		return false
	}
	if containsFeature(c, "未然形") {
		return false
	}
	return true
}

func isWordForDebug(c []string) bool {
	if len(c) == 0 {
		return false
	}
	if len(c) > 1 && c[0] != "名詞" && c[1] == "非自立" {
		return false
	}
	for _, f := range []string{"名詞", "形容詞", "形容動詞", "副詞", "連体詞", "接続詞", "感動詞", "接頭詞", "フィラー"} {
		if c[0] == f {
			if len(c) > 1 && c[1] == "接尾" {
				return false
			}
			return true
		}
	}
	if c[0] == "接頭辞" || (c[0] == "接続詞" && len(c) > 1 && c[1] == "名詞接続") {
		return false
	}
	if c[0] == "形状詞" && !(len(c) > 1 && c[1] == "助動詞語幹") {
		return true
	}
	if c[0] == "代名詞" {
		return true
	}
	if c[0] == "記号" && len(c) > 1 && c[1] == "一般" {
		return true
	}
	if c[0] == "助詞" {
		if len(c) > 1 {
			x := c[1]
			if x == "副助詞" || x == "準体助詞" || x == "終助詞" || x == "係助詞" || x == "格助詞" || x == "接続助詞" || x == "連体化" || x == "副助詞／並立助詞／終助詞" {
				return false
			}
		}
		return true
	}
	if c[0] == "動詞" {
		if len(c) > 1 && (c[1] == "接尾" || c[1] == "非自立") {
			return false
		}
		return true
	}
	if c[0] == "カスタム人名" || c[0] == "カスタム名詞" {
		return true
	}
	return false
}

func posSummary(c []string) string {
	if len(c) == 0 {
		return "unknown"
	}
	if len(c) == 1 {
		return c[0]
	}
	return c[0] + "/" + c[1]
}

func tokenReading(d *dict.Dict, tok tokenizer.Token) string {
	f := tok.Features()
	if reKana.MatchString(tok.Surface) {
		return tok.Surface
	}
	if len(f) == 3 {
		return f[2]
	}
	idx := dictIdx(d, dict.PronunciationIndex)
	if idx >= 0 && idx < len(f) {
		return f[idx]
	}
	return tok.Surface
}

func isIgnoreToken(f []string) bool {
	return len(f) > 0 && (f[0] == "空白" || f[0] == "補助記号" || (len(f) > 1 && f[0] == "記号" && f[1] == "空白"))
}

func dictIdx(d *dict.Dict, typ string) int {
	if ii, ok := d.ContentsMeta[typ]; ok {
		return int(ii)
	}
	return -1
}

func moraUnitBreakdown(reading string) (string, int, int) {
	parts := make([]string, 0, len([]rune(reading)))
	strict := 0
	amb := 0
	for _, r := range reading {
		if reIgnoreChar.MatchString(string(r)) {
			parts = append(parts, fmt.Sprintf("%c:0", r))
			continue
		}
		strict++
		if r == 'ッ' || r == 'ー' {
			amb++
			parts = append(parts, fmt.Sprintf("%c:1?", r))
			continue
		}
		parts = append(parts, fmt.Sprintf("%c:1", r))
	}
	return strings.Join(parts, " "), strict, amb
}

func yesNo(v bool) string {
	if v {
		return "あり"
	}
	return "なし"
}

func trimForDisplay(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func japaneseCharRatio(s string) float64 {
	var total, jp int
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) || r == 'ー' || r == '・' {
			jp++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(jp) / float64(total)
}

var reDiscordTokens = regexp.MustCompile(
	`<@!?\d+>` +
		`|<#\d+>` +
		`|<@&\d+>` +
		`|<a?:\w+:\d+>` +
		`|https?://\S+`,
)

func containsDiscordTokens(s string) bool {
	return reDiscordTokens.MatchString(s)
}

func findHaikuSafe(content string, rule []int) (result []string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("Recovered from panic in haiku.Find", "error", r, "content_len", len(content))
			result = nil
		}
	}()
	return haiku.Find(content, rule)
}

var (
	reFencedCodeBlock = regexp.MustCompile("(?s)```.*?```")
	reInlineCode      = regexp.MustCompile("`[^`]+`")
	reIgnoreText      = regexp.MustCompile(`[\[\]［］「」『』、。？！]`)
	reKana            = regexp.MustCompile(`^[ァ-ヶー]+$`)
	reWord            = regexp.MustCompile(`^[ァ-ヾ]+$`)
)

func stripCodeBlocks(s string) string {
	s = reFencedCodeBlock.ReplaceAllString(s, "")
	s = reInlineCode.ReplaceAllString(s, "")
	return s
}

var reSpoiler = regexp.MustCompile(`\|\|.+?\|\|`)
var reIgnoreChar = regexp.MustCompile(`[ァィゥェォャュョ]`)

func containsSpoiler(s string) bool {
	return reSpoiler.MatchString(s)
}

func stripSpoilerMarkers(s string) string {
	return strings.ReplaceAll(s, "||", "")
}

func haikuSpansNewline(content, haikuResult string) bool {
	if !strings.Contains(content, "\n") {
		return false
	}
	matched := strings.ReplaceAll(haikuResult, " ", "")
	return !strings.Contains(content, matched)
}

const japaneseCharRatioThreshold = 0.5

func isJapaneseRich(s string) bool {
	return japaneseCharRatio(s) >= japaneseCharRatioThreshold
}
