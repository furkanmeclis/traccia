"use client";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { FilterEditor, newFilter } from "./filter-editor";
import { fields, type Query } from "./query";
import type { SearchResource } from "./capture-state";
export function QueryEditor({
  resource,
  value,
  onChange,
  numericInputs,
  onNumericInput,
}: {
  resource: SearchResource;
  value: Query;
  onChange: (next: Query) => void;
  numericInputs: Record<string, string>;
  onNumericInput: (path: string, raw: string) => void;
}) {
  const { t } = useLocale();
  return (
    <div className="min-w-0 space-y-3">
      <label className="grid gap-1">
        <span>{t("searchviews.searchText")}</span>
        <input
          className="rounded border p-2"
          value={value.search}
          onChange={(e) => onChange({ ...value, search: e.target.value })}
        />
      </label>
      <label className="grid gap-1">
        <span>{t("searchviews.archived")}</span>
        <select
          className="rounded border p-2"
          value={value.archived}
          onChange={(e) => {
            const archived = e.target.value;
            if (
              archived === "active" ||
              archived === "archived" ||
              archived === "any"
            )
              onChange({ ...value, archived });
          }}
        >
          {["active", "archived", "any"].map((a) => (
            <option key={a} value={a}>
              {t("searchviews.archive." + a)}
            </option>
          ))}
        </select>
      </label>
      {value.filter ? (
        <>
          <FilterEditor
            resource={resource}
            value={value.filter}
            numericInputs={numericInputs}
            onNumericInput={onNumericInput}
            onChange={(filter) => onChange({ ...value, filter })}
          />
          <Button
            type="button"
            variant="outline"
            onClick={() => onChange({ ...value, filter: null })}
          >
            {t("searchviews.clearFilter")}
          </Button>
        </>
      ) : (
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            onChange({ ...value, filter: newFilter(resource, "all") })
          }
        >
          {t("searchviews.addFilter")}
        </Button>
      )}
      <fieldset className="space-y-2 rounded border p-2">
        <legend>{t("searchviews.sort")}</legend>
        <p className="text-muted-foreground text-xs">
          {t("searchviews.sortHelp")}
        </p>
        {value.sort.map((sort, index) => (
          <div key={index} className="flex flex-wrap items-end gap-2">
            <label className="grid gap-1">
              <span>{t("searchviews.field")}</span>
              <select
                className="rounded border p-2"
                value={sort.field}
                onChange={(e) =>
                  onChange({
                    ...value,
                    sort: value.sort.map((v, i) =>
                      i === index ? { ...v, field: e.target.value } : v,
                    ),
                  })
                }
              >
                {fields[resource].sort.map((field) => (
                  <option key={field} value={field}>
                    {t("searchviews.fields." + field)}
                  </option>
                ))}
              </select>
            </label>
            <label className="grid gap-1">
              <span>{t("searchviews.direction")}</span>
              <select
                className="rounded border p-2"
                value={sort.direction}
                onChange={(e) => {
                  const direction = e.target.value;
                  if (direction === "asc" || direction === "desc")
                    onChange({
                      ...value,
                      sort: value.sort.map((v, i) =>
                        i === index ? { ...v, direction } : v,
                      ),
                    });
                }}
              >
                <option value="asc">{t("searchviews.asc")}</option>
                <option value="desc">{t("searchviews.desc")}</option>
              </select>
            </label>
            <label className="grid gap-1">
              <span>{t("searchviews.nulls")}</span>
              <select
                className="rounded border p-2"
                value={sort.nulls}
                onChange={(e) => {
                  const nulls = e.target.value;
                  if (nulls === "first" || nulls === "last")
                    onChange({
                      ...value,
                      sort: value.sort.map((v, i) =>
                        i === index ? { ...v, nulls } : v,
                      ),
                    });
                }}
              >
                <option value="first">{t("searchviews.first")}</option>
                <option value="last">{t("searchviews.last")}</option>
              </select>
            </label>
            <Button
              type="button"
              variant="outline"
              disabled={index === 0}
              onClick={() => {
                const sort = [...value.sort];
                [sort[index - 1], sort[index]] = [sort[index], sort[index - 1]];
                onChange({ ...value, sort });
              }}
            >
              {t("searchviews.up")}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() =>
                onChange({
                  ...value,
                  sort: value.sort.filter((_, i) => i !== index),
                })
              }
            >
              {t("searchviews.remove")}
            </Button>
          </div>
        ))}
        <Button
          type="button"
          variant="outline"
          disabled={value.sort.length >= 5}
          onClick={() => {
            const field = fields[resource].sort.find(
              (f) => !value.sort.some((s) => s.field === f),
            );
            if (field)
              onChange({
                ...value,
                sort: [
                  ...value.sort,
                  { field, direction: "asc", nulls: "last" },
                ],
              });
          }}
        >
          {t("searchviews.addSort")}
        </Button>
      </fieldset>
    </div>
  );
}
