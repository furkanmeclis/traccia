import { paragraph, type Block } from "./contract.ts";
/** Deliberate canonical fixture shared by the panel-sized widget harness and tests. */
export function documentFixture(): Block[] {
  return [
    {
      id: crypto.randomUUID(),
      type: "h1",
      children: [{ text: "İş 👩🏽‍💻 e\u0301" }],
    },
    {
      id: crypto.randomUUID(),
      type: "p",
      children: [{ text: "A\r\nB\n\n", bold: false }],
      listStyleType: "todo",
      checked: false,
      indent: 1,
    },
    {
      id: crypto.randomUUID(),
      type: "table",
      children: [
        {
          id: crypto.randomUUID(),
          type: "tr",
          children: [
            {
              id: crypto.randomUUID(),
              type: "td",
              children: [paragraph("merged")],
              colSpan: 2,
              rowSpan: 1,
              background: "#fff",
              borders: { top: { color: "#000", size: 1, style: "solid" } },
            },
          ],
        },
      ],
      colSizes: [160, 160],
      marginLeft: 0,
    },
    {
      id: crypto.randomUUID(),
      type: "code_block",
      children: [
        {
          id: crypto.randomUUID(),
          type: "code_line",
          children: [{ text: "const a = 1;" }],
        },
        {
          id: crypto.randomUUID(),
          type: "code_line",
          children: [{ text: "" }],
        },
        {
          id: crypto.randomUUID(),
          type: "code_line",
          children: [{ text: "" }],
        },
      ],
      lang: "ts",
    },
    paragraph("end"),
  ];
}
