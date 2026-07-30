// A document that meets the PDF/UA-1 requirements: it has a title and every
// figure carries alt text.
#set document(title: [Accessible Report], author: "go-typst")
#set text(lang: "en")

= Introduction

This document is exported as a tagged, PDF/UA-1 conformant file.

== Details

- Headings become structure elements.
- Lists and tables are tagged.

#figure(
  image("logo.png", alt: "The project logo"),
  caption: [The logo, with alt text for screen readers.],
)
