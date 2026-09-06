"""スレッド HTML の「新着」の印と開閉の記憶を実ブラウザで検査する。外部サイトへ接続しない。

Chromium は初期表示の <details open> にも toggle を発火するため、開閉を toggle で記録すると
読み込んだ瞬間に全エントリが「操作済み」になり「新着」が一度も出ない(2026-09-06 の不具合)。
ここでは印の個数と開閉の状態を、読み直しをはさんで数える。
"""
import os
from pathlib import Path
import sys

from playwright.sync_api import sync_playwright

root = Path(sys.argv[1]).resolve()
url = (root / "thread.html").as_uri()


def check(label, got, want):
    if got != want:
        raise SystemExit("%s: %r(期待 %r)" % (label, got, want))
    print("%s: %r" % (label, got), flush=True)


with sync_playwright() as p:
    options = {"headless": True}
    if os.environ.get("BRAINDEX_BROWSER_EXECUTABLE"):
        options["executable_path"] = os.environ["BRAINDEX_BROWSER_EXECUTABLE"]
    browser = p.chromium.launch(**options)
    context = browser.new_context(viewport={"width": 1000, "height": 900}, color_scheme="light")
    context.set_default_navigation_timeout(60000)
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda err: errors.append(str(err)))

    badges = lambda: page.locator(".ent-n").count()
    opened = lambda: page.eval_on_selector_all("details.ent", "es=>es.map(e=>e.open)")

    page.goto(url, wait_until="domcontentloaded")
    page.wait_for_load_state("networkidle")
    check("初回表示の新着", badges(), 3)
    check("初回表示の開閉", opened(), [True, True, True])
    check("初回表示で覚えた開閉", page.evaluate("()=>Object.keys(localStorage).filter(k=>k.indexOf('ans-open')===0).length"), 0)

    page.reload(wait_until="domcontentloaded")
    check("読み直した後の新着", badges(), 3)

    page.locator("details.ent >> nth=1 >> summary").click()
    check("2 つ目を畳んだ後の新着", badges(), 2)
    check("2 つ目を畳んだ後の開閉", opened(), [True, False, True])

    page.reload(wait_until="domcontentloaded")
    check("読み直した後の新着(畳んだ 1 件だけ消える)", badges(), 2)
    check("読み直した後の開閉", opened(), [True, False, True])

    page.click("#thr-close")
    check("全部畳んだ後の新着", badges(), 0)
    page.reload(wait_until="domcontentloaded")
    check("読み直した後の新着", badges(), 0)
    check("読み直した後の開閉", opened(), [False, False, False])

    # 消し込みの鍵はエントリごとに分かれている(同じ文言の項目を上に足しても状態が移らない)。
    page.click("#thr-open")
    page.locator("li.task > input[type=checkbox]").first.check()
    keys = page.evaluate("()=>Object.keys(localStorage).filter(k=>k.indexOf('ans-task:')===0)")
    if not keys or "e-20260906T110000" not in keys[0]:
        raise SystemExit("消し込みの鍵にエントリの id が入っていない: %r" % (keys,))
    print("消し込みの鍵: %r" % (keys,), flush=True)

    if errors:
        raise SystemExit("JS のエラー: %r" % (errors,))
    browser.close()
print("ok", flush=True)
