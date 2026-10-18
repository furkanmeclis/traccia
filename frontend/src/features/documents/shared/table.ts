import {
  paragraph,
  validateContent,
  type Table,
  type Cell,
} from "./contract.ts";
/** Split a merged cell without inventing filler leaves in fully covered rows. */
export function splitCellTable(table: Table, cellID: string): Table {
  const copy = structuredClone(table),
    positions = copy.children.map(() => [] as { column: number; cell: Cell }[]),
    occupied: boolean[][] = copy.children.map(() => []);
  let selected: { row: number; column: number; cell: Cell } | undefined;
  copy.children.forEach((row, r) => {
    if (!("children" in row)) throw new Error("Invalid row");
    let column = 0;
    for (const node of row.children) {
      if (!("children" in node)) throw new Error("Invalid cell");
      while (occupied[r][column]) column++;
      positions[r].push({ column, cell: node });
      if (node.id === cellID) selected = { row: r, column, cell: node };
      for (let y = r; y < r + Number(node.rowSpan ?? 1); y++)
        for (let x = column; x < column + Number(node.colSpan ?? 1); x++)
          occupied[y][x] = true;
      column += Number(node.colSpan ?? 1);
    }
  });
  if (!selected) throw new Error("Cell not found");
  const { row, column, cell } = selected,
    rows = Number(cell.rowSpan ?? 1),
    cols = Number(cell.colSpan ?? 1);
  if (rows === 1 && cols === 1) return copy;
  positions[row] = positions[row].filter((x) => x.cell.id !== cellID);
  for (let r = row; r < row + rows; r++)
    for (let c = column; c < column + cols; c++) {
      const node: Cell = {
        ...cell,
        id: r === row && c === column ? cell.id : crypto.randomUUID(),
        children: r === row && c === column ? cell.children : [paragraph()],
      };
      delete node.rowSpan;
      delete node.colSpan;
      positions[r].push({ column: c, cell: node });
    }
  copy.children.forEach((row, r) => {
    if ("children" in row)
      row.children = positions[r]
        .sort((a, b) => a.column - b.column)
        .map((x) => x.cell);
  });
  validateContent([copy]);
  return copy;
}
