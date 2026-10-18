"use client";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { fields, type Column } from "./query";
import type { SearchResource } from "./capture-state";
export function ColumnEditor({
  resource,
  value,
  onChange,
}: {
  resource: SearchResource;
  value: Column[];
  onChange: (columns: Column[]) => void;
}) {
  const { t } = useLocale();
  return (
    <fieldset className="min-w-0 space-y-2 rounded border p-2">
      <legend>{t("searchviews.columns")}</legend>
      <p className="text-muted-foreground text-xs">
        {t("searchviews.columnsHelp")}
      </p>
      {value.map((column, index) => (
        <div key={column.key} className="flex flex-wrap items-end gap-2">
          <span className="min-w-28">
            {t("searchviews.fields." + column.key)}
          </span>
          <label className="grid gap-1">
            <span>{t("searchviews.width")}</span>
            <input
              className="w-24 rounded border p-2"
              type="number"
              min={80}
              max={800}
              step={1}
              value={Number.isFinite(column.width) ? column.width : ""}
              aria-invalid={
                !Number.isInteger(column.width) ||
                column.width < 80 ||
                column.width > 800
              }
              onChange={(e) =>
                onChange(
                  value.map((c, i) =>
                    i === index
                      ? {
                          ...c,
                          width:
                            e.target.value === ""
                              ? NaN
                              : Number(e.target.value),
                        }
                      : c,
                  ),
                )
              }
            />
          </label>
          <Button
            type="button"
            variant="outline"
            disabled={index === 0}
            onClick={() => {
              const next = [...value];
              [next[index - 1], next[index]] = [next[index], next[index - 1]];
              onChange(next);
            }}
          >
            {t("searchviews.up")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={index === value.length - 1}
            onClick={() => {
              const next = [...value];
              [next[index], next[index + 1]] = [next[index + 1], next[index]];
              onChange(next);
            }}
          >
            {t("searchviews.down")}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => onChange(value.filter((_, i) => i !== index))}
          >
            {t("searchviews.remove")}
          </Button>
        </div>
      ))}
      <label className="grid gap-1">
        <span>{t("searchviews.addColumn")}</span>
        <select
          className="rounded border p-2"
          value=""
          onChange={(e) => {
            const key = fields[resource].columns.find(
              (k) => k === e.target.value,
            );
            if (key && !value.some((c) => c.key === key))
              onChange([...value, { key, width: 180 }]);
          }}
        >
          <option value="">{t("searchviews.choose")}</option>
          {fields[resource].columns
            .filter((k) => !value.some((c) => c.key === k))
            .map((key) => (
              <option key={key} value={key}>
                {t("searchviews.fields." + key)}
              </option>
            ))}
        </select>
      </label>
    </fieldset>
  );
}
