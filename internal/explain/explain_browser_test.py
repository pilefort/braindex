"""解説 HTML の目次・グラフ・表を実ブラウザで検査する。外部サイトへ接続しない。

Go のテストからは HTML の形しか見られない。ここで見るのは、実際に描いたときにだけ分かること:
広い画面で目次が本文の横に固定されて重ならないか、読んでいる節に印が付くか、狭い画面で畳まれるか、
棒が伸びるか(prefers-reduced-motion では伸びないか)、広い表がページ全体を横に流していないか。
"""
import os
from pathlib import Path
import re
import sys

from playwright.sync_api import sync_playwright

root = Path(sys.argv[1]).resolve()
url = (root / "explain.html").as_uri()


def check(label, got, want):
    if got != want:
        raise SystemExit("%s: %r(期待 %r)" % (label, got, want))
    print("%s: %r" % (label, got), flush=True)


def ok(label, cond, detail):
    if not cond:
        raise SystemExit("%s: %s" % (label, detail))
    print("%s: %s" % (label, detail), flush=True)


def contrast(a, b):
    """rgb(...) の 2 色のコントラスト比(WCAG 2.1。Go 側の mdhtml.Contrast と同じ式)。"""
    def luminance(s):
        v = [float(x) for x in re.findall(r"[\d.]+", s)[:3]]
        def ch(c):
            c = c / 255
            return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
        return 0.2126 * ch(v[0]) + 0.7152 * ch(v[1]) + 0.0722 * ch(v[2])
    la, lb = luminance(a), luminance(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)


def launch(p):
    options = {"headless": True}
    if os.environ.get("BRAINDEX_BROWSER_EXECUTABLE"):
        options["executable_path"] = os.environ["BRAINDEX_BROWSER_EXECUTABLE"]
    return p.chromium.launch(**options)


def new_page(browser, width, height, **kw):
    context = browser.new_context(viewport={"width": width, "height": height},
                                  color_scheme="light", **kw)
    context.set_default_navigation_timeout(60000)
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda err: errors.append(str(err)))
    page.goto(url, wait_until="domcontentloaded")
    page.wait_for_load_state("networkidle")
    return page, errors


with sync_playwright() as p:
    browser = launch(p)

    # --- 広い画面: 目次は本文の横に固定され、本文と重ならない
    page, errors = new_page(browser, 1440, 900)
    check("JS のエラー", errors, [])
    check("目次の開閉(広い画面)", page.eval_on_selector("#bx-toc", "e=>e.open"), True)
    check("目次の位置", page.eval_on_selector("#bx-toc", "e=>getComputedStyle(e).position"), "fixed")
    toc = page.eval_on_selector("#bx-toc", "e=>e.getBoundingClientRect().right")
    body = page.eval_on_selector(".doc.explain h1", "e=>e.getBoundingClientRect().left")
    # 「重なっていない」だけでは足りない。2026-09-12 に 8px しか空いていないのを指摘された
    ok("目次と本文の間", body - toc >= 40, "%.0f px 空いている(40 px 以上)" % (body - toc))

    # 段落と見出しの右端がそろう。行長を em で指定すると見出しだけ 1.6 倍に広がる(2026-09-12 の指摘)
    sels = ['.doc.explain p', '.doc.explain h1', '.doc.explain h2', '.doc.explain > ul']
    widths = page.evaluate("""(ss)=>ss.map(function(s){
      var e=document.querySelector(s);
      return e?Math.round(e.getBoundingClientRect().width):-1;})""", sels)
    ok("本文の行長がそろう", min(widths) > 0 and max(widths) - min(widths) <= 1,
       "段落・見出し・箇条書きの幅 %r" % widths)

    # --- 読んでいる節に印が付く
    cur = lambda: page.eval_on_selector_all("#bx-toc a.cur", "es=>es.map(e=>e.textContent)")
    check("最初の印", cur(), ["基礎となる概念"])
    page.eval_on_selector("#s2", "e=>e.scrollIntoView()")
    page.wait_for_timeout(200)
    check("スクロール後の印", cur(), ["実験"])

    # --- 図と棒グラフが描かれている(幅・高さが 0 でない)
    figw = page.eval_on_selector(".bx-fig svg", "e=>Math.round(e.getBoundingClientRect().width)")
    ok("図の幅", figw > 100, "%d px" % figw)
    # --- 図の色は本文に追従する。図の側に値を書かないので、ここが効かないと図だけ配色から外れる
    # (2026-09-12: 暗い配色前提の図を明るい本文に入れて、文字と背景の比が 1.13 になった)
    fig_fg = page.eval_on_selector(".bx-fig svg.bxfig #figtext", "e=>getComputedStyle(e).fill")
    body_fg = page.eval_on_selector(".doc.explain p", "e=>getComputedStyle(e).color")
    check("図の文字色は本文と同じ", fig_fg, body_fg)
    bg = page.eval_on_selector("body", "e=>getComputedStyle(e).backgroundColor")
    ok("図の文字が背景から浮く(明るい配色)", contrast(fig_fg, bg) >= 4.5,
       "比 %.2f(4.5 以上)" % contrast(fig_fg, bg))
    page.evaluate("document.documentElement.setAttribute('data-theme','dark')")
    dark_fg = page.eval_on_selector(".bx-fig svg.bxfig #figtext", "e=>getComputedStyle(e).fill")
    dark_bg = page.eval_on_selector("body", "e=>getComputedStyle(e).backgroundColor")
    ok("暗い配色で図の文字色も変わる", dark_fg != fig_fg, "%s → %s" % (fig_fg, dark_fg))
    ok("図の文字が背景から浮く(暗い配色)", contrast(dark_fg, dark_bg) >= 4.5,
       "比 %.2f(4.5 以上)" % contrast(dark_fg, dark_bg))
    page.evaluate("document.documentElement.removeAttribute('data-theme')")

    bars = page.eval_on_selector_all(".bx-bar", "es=>es.map(e=>Math.round(e.getBoundingClientRect().height))")
    ok("棒の本数", len(bars) == 4, "%d 本" % len(bars))
    check("棒が伸びる指定", page.eval_on_selector(".bx-bar", "e=>getComputedStyle(e).animationName"), "bx-grow")
    page.wait_for_timeout(900)  # 伸び終わるまで待つ
    bars = page.eval_on_selector_all(".bx-bar", "es=>es.map(e=>Math.round(e.getBoundingClientRect().height))")
    ok("伸びたあとの棒", all(h > 0 for h in bars), "%r" % bars)

    # --- 広い表はその表だけが横スクロールし、ページ全体は横に流れない
    tw = page.eval_on_selector_all(
        ".bx-tw", "es=>{var e=es[es.length-1];return [e.scrollWidth, e.clientWidth];}")
    ok("表の横スクロール", tw[0] > tw[1], "表の中身 %d px > 表示幅 %d px(箱の中で溢れている)" % (tw[0], tw[1]))
    doc = page.evaluate("[document.documentElement.scrollWidth, document.documentElement.clientWidth]")
    ok("ページの横流れ", doc[0] <= doc[1] + 1, "ページの幅 %d / 表示幅 %d" % (doc[0], doc[1]))

    # --- もっと広い画面: 本文が中央に戻っても、目次との間は空いたまま
    page, errors = new_page(browser, 1800, 900)
    check("JS のエラー(1800px)", errors, [])
    toc = page.eval_on_selector("#bx-toc", "e=>e.getBoundingClientRect().right")
    body = page.eval_on_selector(".doc.explain h1", "e=>e.getBoundingClientRect().left")
    ok("目次と本文の間(1800px)", body - toc >= 40, "%.0f px 空いている" % (body - toc))

    # --- 狭い画面: 目次は本文の先頭に畳まれる
    page, errors = new_page(browser, 820, 900)
    check("JS のエラー(狭い画面)", errors, [])
    check("目次の開閉(狭い画面)", page.eval_on_selector("#bx-toc", "e=>e.open"), False)
    check("目次の位置(狭い画面)", page.eval_on_selector("#bx-toc", "e=>getComputedStyle(e).position"), "static")
    page.eval_on_selector("#bx-toc>summary", "e=>e.click()")
    check("押せば開く", page.eval_on_selector("#bx-toc", "e=>e.open"), True)

    # --- 動きを嫌う設定では棒が伸びない
    page, errors = new_page(browser, 1440, 900, reduced_motion="reduce")
    check("JS のエラー(reduced motion)", errors, [])
    check("棒の動き", page.eval_on_selector(".bx-bar", "e=>getComputedStyle(e).animationName"), "none")
    h = page.eval_on_selector_all(".bx-bar", "es=>es.map(e=>Math.round(e.getBoundingClientRect().height))")
    ok("止めても棒は出ている", all(x > 0 for x in h), "%r" % h)
    check("図の動きも止まる",
          page.eval_on_selector(".bx-fig svg", "e=>e.animationsPaused?e.animationsPaused():null"), True)

    browser.close()
print("ブラウザ検査: すべて通った", flush=True)
