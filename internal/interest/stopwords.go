package interest

// stopwords は関心を表さない語。英語の機能語、日本語の一般語、セッションログに多い道具の語(コマンド・ツール名・
// ファイル形式)を含める。関心の推定に効かない語を落とすためで、網羅は目指さない(足りなければ足す)。
// 較正は作者環境で行い、ここには語だけを置く(2026-09-03)。
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range stopwordList {
		m[w] = true
	}
	return m
}()

var stopwordList = []string{
	// 英語の機能語・一般語
	"the", "and", "for", "are", "but", "not", "you", "all", "any", "can", "had", "her", "was", "one", "our", "out",
	"has", "have", "him", "his", "how", "its", "may", "new", "now", "old", "see", "two", "way", "who", "did", "get",
	"let", "put", "say", "she", "too", "use", "with", "this", "that", "from", "they", "will", "would", "there", "their",
	"what", "about", "which", "when", "make", "like", "time", "just", "know", "take", "into", "year", "your", "some",
	"could", "them", "than", "then", "look", "only", "come", "over", "think", "also", "back", "after", "work", "first",
	"well", "even", "want", "because", "these", "give", "most", "should", "here", "where", "more", "other", "been",
	"were", "each", "does", "done", "same", "such", "very", "much", "many", "need", "still", "using", "used", "via",
	"yes", "okay", "please", "thanks", "thank", "hello", "hi", "note", "notes", "todo", "issue", "issues", "doc", "docs",
	"readme", "example", "examples", "test", "tests", "testing", "update", "updated", "updates", "fix", "fixed", "fixes",
	"add", "added", "adds", "remove", "removed", "change", "changed", "changes", "version", "versions", "release",
	// コマンド・ツール・ファイル形式(セッションログの雑音)
	"bash", "powershell", "cmd", "git", "github", "commit", "commits", "push", "pull", "merge", "branch", "rebase", "diff",
	"grep", "sed", "awk", "cat", "ls", "cd", "mkdir", "rm", "echo", "python", "pip", "npm", "node", "go", "run", "build",
	"claude", "code", "tool", "tools", "agent", "agents", "read", "write", "edit", "glob", "search", "file", "files",
	"dir", "path", "paths", "md", "json", "jsonl", "yaml", "yml", "txt", "csv", "tsv", "html", "css", "xml", "png", "jpg",
	"http", "https", "www", "com", "org", "net", "url", "urls", "localhost", "true", "false", "null", "nil", "none",
	"string", "int", "bool", "func", "var", "const", "type", "return", "import", "package", "main", "err", "error",
	"errors", "args", "arg", "flag", "flags", "config", "settings", "setting", "default", "defaults", "stdout", "stderr",
	"src", "lib", "bin", "tmp", "temp", "home", "user", "users", "root", "work", "docs", "index", "output", "input",
	// 日本語の一般語(漢字・カタカナ)。セッションと索引に頻出で関心を表さないもの
	"場合", "必要", "確認", "実行", "追加", "設定", "修正", "変更", "対応", "作成", "以下", "以上", "可能", "問題", "方法",
	"内容", "結果", "現在", "今回", "前回", "全部", "自分", "理由", "時間", "記録", "決定", "判断", "作業", "実装", "完了",
	"状態", "更新", "削除", "出力", "入力", "指定", "使用", "利用", "参照", "説明", "表示", "処理", "取得", "生成", "検索",
	"最初", "最後", "今日", "明日", "昨日", "本日", "先頭", "末尾", "全体", "部分", "一部", "他方", "本文", "行目", "件目",
	"存在", "有無", "既存", "新規", "手動", "自動", "同じ", "違い", "既定", "定義", "形式", "項目", "一覧", "全件", "各行",
	"整理", "提案", "報告", "依頼", "質問", "回答", "返答", "了解", "承知", "確定", "検討", "想定", "予定", "実際", "重要",
	"日本語", "英語", "文字", "文章", "言葉", "単語", "名前", "番号", "数字", "日付", "時刻", "場所", "位置", "順序", "順番",
	"ファイル", "フォルダ", "ディレクトリ", "コード", "コミット", "ブランチ", "テスト", "エラー", "ツール", "コマンド",
	"データ", "ユーザー", "ユーザ", "セッション", "メッセージ", "チェック", "リスト", "リンク", "パス", "タイトル", "フラグ",
	"オプション", "デフォルト", "バージョン", "レビュー", "メモ", "ノート", "ページ", "サイト", "ボタン", "クリック",
	"インストール", "アップデート", "ダウンロード", "アップロード", "スクリプト", "プロセス", "ログ", "キー", "テキスト",
}
