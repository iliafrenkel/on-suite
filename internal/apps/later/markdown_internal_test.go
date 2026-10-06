package later

import "testing"

func TestHTMLToMarkdown(t *testing.T) {
	images := map[string]string{"/later/img/abc": "https://e.example/cat.jpg"}
	image := func(src string) string { return images[src] }
	cases := []struct {
		name, html string
		shift      int
		want       string
	}{
		{"empty", ``, 0, ``},
		{"paragraphs", `<p>One</p><p>Two</p>`, 0, "One\n\nTwo"},
		{"whitespace collapses", "<p>  a\n  b  </p>", 0, "a b"},
		{"headings", `<h1>Title</h1><h3>Sub <em>part</em></h3>`, 0, "# Title\n\n### Sub *part*"},
		{"headings shift and cap", `<h1>A</h1><h5>B</h5>`, 2, "### A\n\n###### B"},
		{"emphasis keeps spaces outside", `<p>a<em> b </em>c</p>`, 0, "a *b* c"},
		{"strong and strike", `<p><strong>bold</strong> <b>b</b> <del>gone</del> <s>x</s></p>`, 0, "**bold** **b** ~~gone~~ ~~x~~"},
		{"link", `<p>See <a href="https://e.example/a_(b)" rel="nofollow">here</a>.</p>`, 0, "See [here](https://e.example/a_%28b%29)."},
		{"link without text is dropped", `<p><a href="https://e.example/"></a>x</p>`, 0, "x"},
		{"inline escaping", `<p>a*b_c [d] \e &lt;f&gt; ~g</p>`, 0, `a\*b\_c \[d\] \\e \<f> \~g`},
		{"line starts escaped", `<p># not a heading</p><p>1. not a list</p><p>- nor this</p>`, 0, "\\# not a heading\n\n1\\. not a list\n\n\\- nor this"},
		{"br is a hard break", `<p>line one<br>line two</p>`, 0, "line one\\\nline two"},
		{"inline code", `<p>Run <code>go test</code> and <kbd>Ctrl</kbd></p>`, 0, "Run `go test` and `Ctrl`"},
		{"code containing a backtick", "<p><code>a`b</code></p>", 0, "``a`b``"},
		{"pre is fenced and unescaped", "<pre><code>func main() {\n\tprintln(\"*\")\n}\n</code></pre>", 0, "```\nfunc main() {\n\tprintln(\"*\")\n}\n```"},
		{"blockquote", `<blockquote><p>One</p><p>Two</p></blockquote>`, 0, "> One\n>\n> Two"},
		{"unordered list", `<ul><li>One</li><li>Two <em>b</em></li></ul>`, 0, "- One\n- Two *b*"},
		{"nested ordered list", `<ol><li>First<ul><li>Inner</li></ul></li><li>Second</li></ol>`, 0, "1. First\n\n   - Inner\n2. Second"},
		{"definition list", `<dl><dt>Term</dt><dd>Meaning</dd></dl>`, 0, "**Term**\n\nMeaning"},
		{"table", `<table><caption>Scores</caption><thead><tr><th>Name</th><th>Pts</th></tr></thead><tbody><tr><td>A|B</td><td>3</td></tr><tr><td>C</td></tr></tbody></table>`, 0,
			"Scores\n\n| Name | Pts |\n| --- | --- |\n| A\\|B | 3 |\n| C |  |"},
		{"figure with image", `<figure><img src="/later/img/abc" alt="A cat"><figcaption>Our cat</figcaption></figure>`, 0, "[A cat](https://e.example/cat.jpg)\n\nOur cat"},
		{"image without alt", `<p><img src="/later/img/abc"></p>`, 0, "[Image](https://e.example/cat.jpg)"},
		{"unknown image keeps only its alt", `<p><img src="/later/img/zzz" alt="">Text <img src="/later/img/zzz" alt="Gone"></p>`, 0, "Text Gone"},
		{"text-only inline elements", `<p><q>Hi</q> <span>there</span> <abbr title="x">HTML</abbr> <sup>2</sup></p>`, 0, "“Hi” there HTML 2"},
		{"rule", `<p>a</p><hr><p>b</p>`, 0, "a\n\n---\n\nb"},
		{"div mixes inline and blocks", `<div>Loose text<p>Para</p>tail</div>`, 0, "Loose text\n\nPara\n\ntail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := htmlToMarkdown(c.html, image, c.shift); got != c.want {
				t.Errorf("htmlToMarkdown(%q)\n got: %q\nwant: %q", c.html, got, c.want)
			}
		})
	}
}
