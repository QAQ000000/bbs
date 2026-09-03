// SPDX-License-Identifier: AGPL-3.0-or-later
package smiley

// builtinList 内置 Unicode emoji 短代码（默认表情面板）。
// 短代码命名沿用社区通用惯例（gemoji/GitHub 风格），字符为 Unicode 标准内容，
// 渲染为字符本身，不含任何第三方素材文件。
var builtinList = []Code{
	{Code: ":smile:", Unicode: "😄"}, {Code: ":joy:", Unicode: "😂"},
	{Code: ":grin:", Unicode: "😁"}, {Code: ":wink:", Unicode: "😉"},
	{Code: ":blush:", Unicode: "😊"}, {Code: ":slightly_smiling_face:", Unicode: "🙂"},
	{Code: ":upside_down_face:", Unicode: "🙃"}, {Code: ":relaxed:", Unicode: "☺️"},
	{Code: ":yum:", Unicode: "😋"}, {Code: ":stuck_out_tongue:", Unicode: "😛"},
	{Code: ":stuck_out_tongue_closed_eyes:", Unicode: "😝"},
	{Code: ":zany_face:", Unicode: "🤪"}, {Code: ":face_with_raised_eyebrow:", Unicode: "🤨"},
	{Code: ":neutral_face:", Unicode: "😐"}, {Code: ":expressionless:", Unicode: "😑"},
	{Code: ":no_mouth:", Unicode: "😶"}, {Code: ":smirk:", Unicode: "😏"},
	{Code: ":unamused:", Unicode: "😒"}, {Code: ":roll_eyes:", Unicode: "🙄"},
	{Code: ":grimacing:", Unicode: "😬"}, {Code: ":lying_face:", Unicode: "🤥"},
	{Code: ":relieved:", Unicode: "😌"}, {Code: ":pensive:", Unicode: "😔"},
	{Code: ":sleepy:", Unicode: "😪"}, {Code: ":drooling_face:", Unicode: "🤤"},
	{Code: ":sleeping:", Unicode: "😴"}, {Code: ":mask:", Unicode: "😷"},
	{Code: ":face_with_thermometer:", Unicode: "🤒"}, {Code: ":dizzy_face:", Unicode: "😵"},
	{Code: ":sunglasses:", Unicode: "😎"}, {Code: ":nerd_face:", Unicode: "🤓"},
	{Code: ":confused:", Unicode: "😕"}, {Code: ":worried:", Unicode: "😟"},
	{Code: ":slightly_frowning_face:", Unicode: "🙁"}, {Code: ":frowning:", Unicode: "😦"},
	{Code: ":open_mouth:", Unicode: "😮"}, {Code: ":hushed:", Unicode: "😯"},
	{Code: ":astonished:", Unicode: "😲"}, {Code: ":flushed:", Unicode: "😳"},
	{Code: ":fearful:", Unicode: "😨"}, {Code: ":cold_sweat:", Unicode: "😰"},
	{Code: ":disappointed_relieved:", Unicode: "😥"}, {Code: ":cry:", Unicode: "😢"},
	{Code: ":sob:", Unicode: "😭"}, {Code: ":fear2:", Unicode: "😱"},
	{Code: ":confounded:", Unicode: "😖"}, {Code: ":persevere:", Unicode: "😣"},
	{Code: ":disappointed:", Unicode: "😞"}, {Code: ":sweat:", Unicode: "😓"},
	{Code: ":weary:", Unicode: "😩"}, {Code: ":tired_face:", Unicode: "😫"},
	{Code: ":angry:", Unicode: "😠"}, {Code: ":rage:", Unicode: "😡"},
	{Code: ":triumph:", Unicode: "😤"}, {Code: ":sleepy2:", Unicode: "😪"},
	{Code: ":thumbsup:", Unicode: "👍"}, {Code: ":thumbsdown:", Unicode: "👎"},
	{Code: ":ok_hand:", Unicode: "👌"}, {Code: ":clap:", Unicode: "👏"},
	{Code: ":wave:", Unicode: "👋"}, {Code: ":pray:", Unicode: "🙏"},
	{Code: ":muscle:", Unicode: "💪"}, {Code: ":v:", Unicode: "✌️"},
	{Code: ":heart:", Unicode: "❤️"}, {Code: ":broken_heart:", Unicode: "💔"},
	{Code: ":sparkling_heart:", Unicode: "💖"}, {Code: ":star:", Unicode: "⭐"},
	{Code: ":sparkles:", Unicode: "✨"}, {Code: ":fire:", Unicode: "🔥"},
	{Code: ":rocket:", Unicode: "🚀"}, {Code: ":tada:", Unicode: "🎉"},
	{Code: ":100:", Unicode: "💯"}, {Code: ":thinking:", Unicode: "🤔"},
	{Code: ":shushing_face:", Unicode: "🤫"},
}

var builtinByCode = func() map[string]Code {
	m := make(map[string]Code, len(builtinList))
	for _, c := range builtinList {
		m[c.Code] = c
	}
	return m
}()
