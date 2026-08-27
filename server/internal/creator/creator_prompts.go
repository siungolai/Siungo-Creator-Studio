package creator

import (
	"context"
	"strings"
)

// 生成提示词（PRD §4.5：系统提示内嵌短视频结构，按风格模板与目标平台微调；JSON 强约束）。
// 质量迭代入口：M2 后按实际生成效果调整此处；Issue 18 起支持「AI 设置」覆盖（覆盖值存 settings 表）。
//
// 注意：JSON 输出契约（OutputContract）与可自定义内容（DefaultSystemPromptBody）分离，
// 自定义提示词仅替换 Body 部分，契约由服务端强制附加——防止自定义提示词破坏解析器兼容。

// DefaultSystemPromptBody 内置默认系统提示词的内容/风格部分（「恢复默认」的基准值）。
const DefaultSystemPromptBody = `你是短视频内容创作助手。根据用户给出的主题与风格要求，生成一套完整的短视频内容。`

// OutputContract JSON 输出契约（强制附加在系统提示词末尾，不可被自定义提示词移除）。
const OutputContract = `必须严格输出 JSON（不要输出 JSON 以外的任何内容、不要 markdown 围栏），结构如下：
{
  "titles": ["标题候选1", "标题候选2", "标题候选3"],
  "script": "脚本正文：开头钩子（3 秒抓住注意力）→ 主体（3-5 个要点，节奏明快）→ 结尾 CTA（引导关注/评论/收藏）",
  "voiceover": "口播稿：口语化、与脚本对应的朗读文本",
  "tags": ["标签1", "标签2", "标签3"],
  "cover_copy": "封面文案：短句、醒目、有吸引力"
}
要求：标题候选 3-5 个；脚本是完整正文而非提纲；口播稿适合真人朗读。`

// DefaultGenerateSystemPrompt 内置完整默认系统提示词（= 内容部分 + 输出契约；对外展示用）。
const DefaultGenerateSystemPrompt = DefaultSystemPromptBody + "\n" + OutputContract

// DefaultGenerateUserPromptTemplate 内置默认用户提示模板（支持 {topic}/{style}/{platform} 占位符）。
const DefaultGenerateUserPromptTemplate = "主题：{topic}"

// 风格模板微调（CONTEXT.md：default=通用缺省）。
var styleHints = map[string]string{
	"default":  "",
	"ganhuo":   "风格：干货型——专业、信息密度高，多用数据/案例/方法论支撑，理性说服。",
	"juqing":   "风格：剧情型——有故事冲突与悬念，先设悬念再揭晓，情绪起伏强。",
	"zhongcao": "风格：种草型——真实体验口吻，突出痛点-解决方案-效果，促进下单欲望。",
	"tucao":    "风格：吐槽型——幽默反讽、犀利犀利，用夸张和段子制造笑点，结尾反转。",
}

// 平台微调（Q19：首版通用文案 + 发布前可针对平台生成版本）。
var platformHints = map[string]string{
	"":         "",
	"douyin":   "平台：抖音——开头 3 秒必须极强钩子，整体节奏快，字幕式表达，结尾强引导关注。",
	"bilibili": "平台：B站——可以稍长更完整，注重封面文案吸引力与弹幕互动点，结尾引导三连。",
}

// buildGeneratePrompts 组装系统提示与用户提示。
// 系统提示 = 覆盖值（设置）或内置 Body + 强制输出契约 + 风格/平台微调追加；
// 用户提示 = 模板（设置或内置默认）做 {topic}/{style}/{platform} 占位替换。
// 设置读取失败一律回退内置默认（个人工具可用性优先）。
func (s *Service) buildGeneratePrompts(ctx context.Context, topic, style, platform string) (systemPrompt, userPrompt string) {
	systemPrompt = DefaultSystemPromptBody
	if s.promptSettings != nil {
		if v, err := s.promptSettings.GenerateSystemPrompt(ctx); err == nil && strings.TrimSpace(v) != "" {
			systemPrompt = v
		}
	}
	// JSON 输出契约强制附加：自定义提示词只影响内容与风格，不得破坏解析器兼容（Issue 18 防护）
	systemPrompt += "\n" + OutputContract
	if h := styleHints[style]; h != "" {
		systemPrompt += "\n" + h
	}
	if h := platformHints[platform]; h != "" {
		systemPrompt += "\n" + h
	}

	tpl := DefaultGenerateUserPromptTemplate
	if s.promptSettings != nil {
		if v, err := s.promptSettings.GenerateUserPromptTemplate(ctx); err == nil && strings.TrimSpace(v) != "" {
			tpl = v
		}
	}
	userPrompt = expandPromptTemplate(tpl, topic, style, platform)
	return systemPrompt, userPrompt
}

// expandPromptTemplate 占位符替换（strings.ReplaceAll 足够，不引入模板引擎；未知占位符原样保留）。
func expandPromptTemplate(tpl, topic, style, platform string) string {
	r := strings.NewReplacer(
		"{topic}", topic,
		"{style}", style,
		"{platform}", platform,
	)
	return r.Replace(tpl)
}
