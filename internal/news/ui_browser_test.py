"""実際の生成HTMLで保存・相談・失敗時の復旧を検査する。外部サイトへ接続しない。"""
import json
import os
from pathlib import Path
import sys

from playwright.sync_api import sync_playwright, expect

root = Path(sys.argv[1]).resolve()
with sync_playwright() as p:
    options = {"headless": True}
    if os.environ.get("BRAINDEX_BROWSER_EXECUTABLE"):
        options["executable_path"] = os.environ["BRAINDEX_BROWSER_EXECUTABLE"]
    browser = p.chromium.launch(**options)
    context = browser.new_context(viewport={"width": 1365, "height": 950}, color_scheme="light")
    context.set_default_navigation_timeout(60000)
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda err: errors.append(str(err)))
    page.goto((root / "digest.html").as_uri(), wait_until="domcontentloaded")
    page.wait_for_load_state("networkidle")
    print("loaded generated HTML", flush=True)
    expect(page.locator(".item:visible")).to_have_count(3)
    page.screenshot(path=str(root / "desktop.png"), full_page=True)
    first = page.locator('[data-id="article-1"]')
    first.locator(".bk").click()
    expect(page.locator("#nK")).to_have_text("1")
    page.locator('[data-view="keep"]').click()
    expect(page.locator(".item:visible")).to_have_count(1)
    first.locator(".reading-status").select_option("hold")
    first.locator(".explain").click()
    page.locator('[name="mode"][value="stuck"]').check()
    page.locator("#question").fill("用語から分からない。具体例で教えて。")
    page.locator("#prepareQuestion").click()
    prompt = page.locator("#requestText").input_value()
    assert "https://example.com/1" in prompt and "news reading -id article-1" in prompt
    page.screenshot(path=str(root / "explain.png"), full_page=True)
    # 同じ未回答の相談を二重に作らない。
    page.locator("#prepareQuestion").click()
    expect(page.locator("#history details")).to_have_count(1)
    page.evaluate("Object.defineProperty(navigator, 'clipboard', {configurable:true, value:{writeText:async()=>{throw new Error('denied')}}})")
    page.locator("#copyRequest").click()
    expect(page.locator("#copyStatus")).to_contain_text("自動コピーできませんでした")
    page.keyboard.press("Escape")
    print("selection and question checks passed", flush=True)
    expect(first.locator(".explain")).to_be_focused()
    # ファイル保存の成功と、その間に操作が変わった場合を検査。
    page.evaluate("() => {window.showSaveFilePicker=async()=>({name:'selection.json',createWritable:async()=>({write:async b=>{window.savedBody=b},close:async()=>{}})});}")
    # 候補は閉じて始まり、記事の絞り込みでも消えない。
    expect(page.locator("section.suggest")).to_be_visible()
    assert page.locator("details.suggest").get_attribute("open") is None
    page.locator("details.suggest > summary").click()
    candidate = page.locator(".suggest-item:not(.query) .ba")
    candidate.click()
    page.locator("#exp").click()
    expect(page.locator("#saveState")).to_contain_text("ファイル保存済み")
    assert json.loads(page.evaluate("window.savedBody"))["add_feeds"] == [{"url":"https://example.com/rss"}]
    candidate.click()
    expect(page.locator("#saveState")).to_contain_text("保存後に変更")
    page.locator("#exp").click()
    assert json.loads(page.evaluate("window.savedBody"))["add_feeds"] == []
    page.locator(".query .ba").click()
    candidate.click()
    page.locator("#exp").click()
    feeds = json.loads(page.evaluate("window.savedBody"))["add_feeds"]
    assert [f["url"] for f in feeds] == sorted(f["url"] for f in feeds)
    assert feeds[1]["query"] == "unlistedword"
    assert "q=unlistedword" in feeds[1]["url"]
    assert "query" not in feeds[0]

    saved = json.loads(page.evaluate("window.savedBody"))
    assert len(saved["keeps"]) == 1 and saved["reading"][0]["status"] == "hold"
    assert len(saved["reading"][0]["questions"]) == 1
    assert saved["feed_stats"]["開発だより"]["dropped"] == 0
    first.locator(".reading-status").select_option("try")
    expect(page.locator("#saveState")).to_contain_text("保存後に変更")
    # 書き込み失敗をダウンロード成功で隠さない。
    page.evaluate("() => {window.showSaveFilePicker=async()=>({createWritable:async()=>{throw new Error('disk full')}});}")
    page.locator("#exp").click()
    expect(page.locator("#notice")).to_contain_text("保存できませんでした")
    expect(page.locator("#saveState")).to_contain_text("保存後に変更")
    page.evaluate("() => {window.showSaveFilePicker=async()=>{throw new DOMException('cancel','AbortError')};}")
    page.locator("#exp").click()
    expect(page.locator("#notice")).to_contain_text("保存を取り消しました")
    # 非対応ブラウザのダウンロードは『取り込み済み』と表示しない。
    page.evaluate("() => {window.showSaveFilePicker=undefined;}")
    with page.expect_download():
        page.locator("#exp").click()
    expect(page.locator("#saveState")).to_contain_text("取り込みは未確認")
    print("save, cancellation and failure checks passed", flush=True)
    page.reload()
    expect(page.locator("#nK")).to_have_text("1")
    expect(page.locator(".ba[aria-pressed=true]")).to_have_count(2)
    expect(page.locator("#saveState")).to_contain_text("取り込みは未確認")
    expect(first.locator(".reading-status")).to_have_value("try")
    page.locator('[data-view="today"]').click()
    page.locator('[data-id="article-2"] .bd').click()
    # 見送った記事が消えても、フォーカスは同じ位置の記事に移る(末尾だったので 1 つ前の article-1)。
    # 上部のビュー切り替えへ飛ばすと、画面が先頭まで戻ってしまう。
    expect(page.locator('[data-id="article-1"] .bd')).to_be_focused()
    page.locator('[data-view="drop"]').click()
    expect(page.locator(".item:visible")).to_have_count(1)
    page.locator('[data-id="article-2"] .bd').click()
    expect(page.locator("#empty")).to_be_visible()
    page.locator('[data-view="today"]').click()
    page.locator('button[data-category="科学"]').click()
    expect(page.locator("section.suggest")).to_be_visible()
    expect(page.locator(".item:visible")).to_have_count(1)
    # 「あとで読む」に入れた記事は、まとめて概要だけを頼める(詳しい解説は記事ごとのダイアログから)。
    page.locator('[data-id="article-3"] .bk').click()
    expect(page.locator("#nBatch")).to_have_text("2")
    page.locator("#batchExplain").click()
    batch = page.locator("#requestText").input_value()
    assert "1. 記事:" in batch and "2. 記事:" in batch, batch
    assert "概要は 1 枚の HTML にまとめてください" in batch, batch
    assert "news reading -id article-1 -question" in batch, batch
    expect(page.locator("#askOne")).to_be_hidden()
    page.keyboard.press("Escape")
    # 概要を読んだあと「詳しく知りたい」にした記事だけを、まとめて頼める。
    expect(page.locator("#deepExplain")).to_be_disabled()
    page.locator('[data-view="keep"]').click()
    # 仕分けの残りが見える（article-3 を入れた直後は「あとで読む」のまま。article-1 は前段で「試したい」にした）。
    expect(page.locator("#visibleCount")).to_contain_text("まだ仕分けていない 1 件")
    page.locator('[data-id="article-3"] .reading-status').select_option("deep")
    expect(page.locator("#visibleCount")).to_contain_text("まだ仕分けていない 0 件")
    expect(page.locator("#nDeep")).to_have_text("1")
    # digest の下書きでも「詳しく知りたい」は再読み込み後に残る。
    page.reload()
    expect(page.locator("#nDeep")).to_have_text("1")
    expect(page.locator('[data-id="article-3"] .reading-status')).to_have_value("deep")
    page.locator("#deepExplain").click()
    deep = page.locator("#requestText").input_value()
    assert "1 件ずつ詳しく解説してください" in deep, deep
    assert "回答は記事ごとに 1 枚の HTML" in deep, deep
    assert "news reading -id article-3 -question" in deep, deep
    assert "article-1" not in deep, deep
    page.keyboard.press("Escape")
    print("batch overview and deep checks passed", flush=True)
    # 「興味なし」も再読み込みで「あとで読む」に戻らない。
    page.locator('[data-view="keep"]').click()
    first.locator(".reading-status").select_option("none")
    page.reload()
    expect(first.locator(".reading-status")).to_have_value("none")
    expect(page.locator("#nDeep")).to_have_text("1")
    page.set_viewport_size({"width": 390, "height": 844})
    page.screenshot(path=str(root / "mobile.png"), full_page=True)
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    page.emulate_media(color_scheme="dark", reduced_motion="reduce")
    page.screenshot(path=str(root / "dark.png"), full_page=True)
    print("responsive and dark checks passed", flush=True)
    # 保存済みの解説を読み、追加質問に以前のやり取りを含められる。
    page.goto((root / "reading.html").as_uri(), wait_until="domcontentloaded")
    page.locator(".explain").click()
    page.locator("#history summary").click()
    expect(page.locator(".answer")).to_contain_text("プログラムが作業中")
    page.locator("#question").fill("もう少し身近な例で")
    page.locator("#prepareQuestion").click()
    assert "プログラムが作業中" in page.locator("#requestText").input_value()
    # 概要の画面: 記事ごとに仕分け、詳しく知りたい分だけの相談文を作る。仕分けは開き直しても残る。
    page.goto((root / "overview.html").as_uri(), wait_until="domcontentloaded")
    expect(page.locator(".art")).to_have_count(2)
    expect(page.locator("#ask")).to_be_disabled()
    expect(page.locator("#progress")).to_contain_text("2 件中 0 件を仕分け済み")
    page.locator('.art[data-id="abc123"] input[value="deep"]').check()
    page.locator('.art[data-id="def456"] input[value="none"]').check()
    expect(page.locator("#n")).to_have_text("1")
    expect(page.locator("#progress")).to_contain_text("2 件中 2 件を仕分け済み")
    page.locator("#ask").click()
    req = page.locator("#req").input_value()
    assert "news reading -id abc123 -ask detail" in req, req
    assert "def456" not in req, req
    assert "1 件ずつ詳しく解説してください" in req, req
    page.locator("#close").click()
    page.reload()
    expect(page.locator("#n")).to_have_text("1")
    expect(page.locator('.art[data-id="abc123"] input[value="deep"]')).to_be_checked()
    print("overview checks passed", flush=True)

    # ストレージの破損・利用不可でも読書とファイルへの退避は続けられる。
    page.close()
    page = context.new_page()
    page.on("pageerror", lambda err: errors.append(str(err)))
    page.add_init_script("Storage.prototype.getItem=function(){return '{broken'};Storage.prototype.setItem=function(){throw new Error('quota')}")
    page.goto((root / "digest.html").as_uri(), wait_until="domcontentloaded")
    expect(page.locator(".item:visible")).to_have_count(3)
    page.locator('[data-id="article-1"] .bk').click()
    expect(page.locator("#saveState")).to_contain_text("下書きを保存できません")
    assert not errors, errors
    context.close()
    browser.close()
print("Browser checks passed: reading, questions, export, errors, keyboard, responsive, dark theme")
