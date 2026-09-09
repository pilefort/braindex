"""生成した HTML を実ブラウザで開き、README 用の PNG を撮る。外部サイトへは接続しない。

    python screenshots.py <HTMLのあるディレクトリ> <PNGの出力先>

呼び出しは screenshots_test.go(BRAINDEX_SCREENSHOT=1)から。
"""
import os
from pathlib import Path
import sys

from playwright.sync_api import sync_playwright

src = Path(sys.argv[1]).resolve()
out = Path(sys.argv[2]).resolve()
out.mkdir(parents=True, exist_ok=True)

# (HTML, PNG, 横幅, 縦の上限px。None は全体)
PAGES = [
    ("approvals.html", "approvals-form.png", 1100, 1400),
    ("news-overview.html", "news-overview.png", 1100, 1200),
    ("news-digest.html", "news-digest.png", 1100, 1200),
    ("news-reading.html", "news-reading.png", 1100, 1200),
    ("answer.html", "answer.png", 1000, 1200),
    ("answer-thread.html", "answer-thread.png", 1000, 1200),
]

with sync_playwright() as p:
    options = {"headless": True}
    if os.environ.get("BRAINDEX_BROWSER_EXECUTABLE"):
        options["executable_path"] = os.environ["BRAINDEX_BROWSER_EXECUTABLE"]
    browser = p.chromium.launch(**options)
    errors = []
    for name, png, width, limit in PAGES:
        context = browser.new_context(
            viewport={"width": width, "height": 900},
            device_scale_factor=2,
            color_scheme="light",
        )
        page = context.new_page()
        page.on("pageerror", lambda err, n=name: errors.append(f"{n}: {err}"))
        page.goto((src / name).as_uri(), wait_until="domcontentloaded")
        page.wait_for_load_state("networkidle")
        # 画面に貼り付く要素(送信ボタン等)が正しい位置に写るよう、表示領域そのものを
        # 中身の高さ(上限つき)に合わせてから撮る。full_page で撮ると貼り付く要素が中途半端な位置に写る。
        height = page.evaluate("document.documentElement.scrollHeight")
        shot = min(height, limit) if limit else height
        page.set_viewport_size({"width": width, "height": shot})
        page.wait_for_timeout(200)
        page.screenshot(path=str(out / png))
        print(f"{png} ({width}x{shot})", flush=True)
        context.close()
    browser.close()
    if errors:
        raise SystemExit("ページ内でエラー: " + "; ".join(errors))
