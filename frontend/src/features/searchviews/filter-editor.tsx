"use client";
import { useId } from "react";
import { Button } from "@/components/ui/button";
import { DatePicker } from "@/components/ui/date-picker";
import { useLocale } from "@/providers/locale-provider";
import { fields, priorities, comparisons, type Filter } from "./query";
import type { SearchResource } from "./capture-state";
const priorityLabels: Record<string, string> = {
  none: "issues.priority_none",
  urgent: "issues.priority_urgent",
  high: "issues.priority_high",
  normal: "issues.priority_normal",
  low: "issues.priority_low",
};
function available(resource: SearchResource) {
  return [
    "all",
    "any",
    "not",
    "uuid",
    "text",
    ...(resource === "issue" ? ["enum", "number"] : []),
    ...(fields[resource].date.length ? ["date"] : []),
    ...(fields[resource].nullable.length ? ["null"] : []),
  ] as Filter["kind"][];
}
export function newFilter(
  resource: SearchResource,
  kind: Filter["kind"],
): Filter {
  switch (kind) {
    case "all":
    case "any":
      return { kind, children: [newFilter(resource, "text")] };
    case "not":
      return { kind, child: newFilter(resource, "text") };
    case "uuid":
      return { kind, field: fields[resource].uuid[0], op: "in", ids: [] };
    case "text":
      return {
        kind,
        field: fields[resource].text[0],
        op: "contains",
        value: "",
      };
    case "enum":
      return { kind, field: "priority", op: "in", values: ["normal"] };
    case "number":
      return { kind, field: "effort", op: "eq", number: 0 };
    case "date":
      return { kind, field: fields[resource].date[0], op: "eq", value: "" };
    case "null":
      return { kind, field: fields[resource].nullable[0], op: "is_null" };
  }
}
function IntegerInput({
  value,
  raw,
  onRaw,
  onChange,
}: {
  value: number;
  raw: string;
  onRaw: (raw: string) => void;
  onChange: (value: number) => void;
}) {
  const { t } = useLocale();
  return (
    <label className="grid gap-1">
      <span>{t("searchviews.number")}</span>
      <input
        className="rounded border p-2"
        aria-invalid={
          !/^-?\d+$/.test(raw) ||
          !Number.isInteger(value) ||
          value < -2147483648 ||
          value > 2147483647
        }
        value={raw}
        inputMode="numeric"
        onChange={(e) => {
          onChange(
            /^-?\d+$/.test(e.target.value) ? Number(e.target.value) : NaN,
          );
          onRaw(e.target.value);
        }}
      />
    </label>
  );
}
export function FilterEditor({
  resource,
  value,
  onChange,
  depth = 1,
  path = "filter",
  numericInputs,
  onNumericInput,
}: {
  resource: SearchResource;
  value: Filter;
  onChange: (value: Filter) => void;
  depth?: number;
  path?: string;
  numericInputs: Record<string, string>;
  onNumericInput: (path: string, raw: string) => void;
}) {
  const { t } = useLocale(),
    id = useId();
  const fieldOptions =
    value.kind === "uuid"
      ? fields[resource].uuid
      : value.kind === "text"
        ? fields[resource].text
        : value.kind === "date"
          ? fields[resource].date
          : value.kind === "null"
            ? fields[resource].nullable
            : [];
  const ops =
    value.kind === "uuid" || value.kind === "enum"
      ? ["in", "not_in"]
      : value.kind === "text"
        ? ["contains", "eq"]
        : value.kind === "null"
          ? ["is_null", "is_not_null"]
          : comparisons;
  return (
    <fieldset className="min-w-0 space-y-2 rounded border p-2">
      <legend>
        {t("searchviews.filter")} {depth}
      </legend>
      <label className="grid gap-1">
        <span>{t("searchviews.kind")}</span>
        <select
          className="rounded border p-2"
          value={value.kind}
          onChange={(e) => {
            const kind = available(resource).find((k) => k === e.target.value);
            if (kind) onChange(newFilter(resource, kind));
          }}
        >
          {available(resource).map((kind) => (
            <option key={kind} value={kind}>
              {t("searchviews.kinds." + kind)}
            </option>
          ))}
        </select>
      </label>
      {(value.kind === "all" || value.kind === "any") && (
        <>
          <div className="space-y-2">
            {value.children.map((child, index) => (
              <div key={index} className="space-y-1">
                <FilterEditor
                  resource={resource}
                  value={child}
                  depth={depth + 1}
                  path={path + "." + index}
                  numericInputs={numericInputs}
                  onNumericInput={onNumericInput}
                  onChange={(next) =>
                    onChange({
                      ...value,
                      children: value.children.map((v, i) =>
                        i === index ? next : v,
                      ),
                    })
                  }
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() =>
                    onChange({
                      ...value,
                      children: value.children.filter((_, i) => i !== index),
                    })
                  }
                >
                  {t("searchviews.removeFilter")}
                </Button>
              </div>
            ))}
          </div>
          <Button
            type="button"
            variant="outline"
            disabled={depth >= 8}
            onClick={() =>
              onChange({
                ...value,
                children: [...value.children, newFilter(resource, "text")],
              })
            }
          >
            {t("searchviews.addFilter")}
          </Button>
        </>
      )}
      {value.kind === "not" && (
        <FilterEditor
          resource={resource}
          value={value.child}
          depth={depth + 1}
          path={path + ".not"}
          numericInputs={numericInputs}
          onNumericInput={onNumericInput}
          onChange={(child) => onChange({ ...value, child })}
        />
      )}
      {"field" in value && (
        <div className="grid gap-2 sm:grid-cols-2">
          <label className="grid gap-1">
            <span>{t("searchviews.field")}</span>
            {fieldOptions.length ? (
              <select
                className="rounded border p-2"
                value={value.field}
                onChange={(e) => {
                  const field = fieldOptions.find((v) => v === e.target.value);
                  if (
                    field &&
                    (value.kind === "uuid" ||
                      value.kind === "text" ||
                      value.kind === "date" ||
                      value.kind === "null")
                  )
                    onChange({ ...value, field });
                }}
              >
                {fieldOptions.map((field) => (
                  <option key={field} value={field}>
                    {t("searchviews.fields." + field)}
                  </option>
                ))}
              </select>
            ) : (
              <span>{t("searchviews.fields." + value.field)}</span>
            )}
          </label>
          <label className="grid gap-1">
            <span>{t("searchviews.operator")}</span>
            <select
              className="rounded border p-2"
              value={value.op}
              onChange={(e) => {
                const op = e.target.value;
                if (
                  (value.kind === "uuid" || value.kind === "enum") &&
                  (op === "in" || op === "not_in")
                )
                  onChange({ ...value, op });
                else if (
                  value.kind === "text" &&
                  (op === "contains" || op === "eq")
                )
                  onChange({ ...value, op });
                else if (
                  value.kind === "null" &&
                  (op === "is_null" || op === "is_not_null")
                )
                  onChange({ ...value, op });
                else if (
                  (value.kind === "number" || value.kind === "date") &&
                  comparisons.some((c) => c === op)
                ) {
                  const found = comparisons.find((c) => c === op);
                  if (found) onChange({ ...value, op: found });
                }
              }}
            >
              {ops.map((op) => (
                <option key={op} value={op}>
                  {t("searchviews.operators." + op)}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
      {value.kind === "text" && (
        <label className="grid gap-1">
          <span>{t("searchviews.value")}</span>
          <input
            className="rounded border p-2"
            value={value.value}
            onChange={(e) => onChange({ ...value, value: e.target.value })}
          />
        </label>
      )}
      {value.kind === "uuid" && (
        <label className="grid gap-1">
          <span>{t("searchviews.ids")}</span>
          <textarea
            className="min-h-20 rounded border p-2 font-mono"
            value={value.ids.join("\n")}
            onChange={(e) =>
              onChange({ ...value, ids: e.target.value.split("\n") })
            }
          />
          <span className="text-muted-foreground text-xs">
            {t("searchviews.idsHelp")}
          </span>
        </label>
      )}
      {value.kind === "enum" && (
        <fieldset>
          <legend>{t("searchviews.value")}</legend>
          <div className="flex flex-wrap gap-3">
            {priorities.map((priority) => (
              <label key={priority} className="flex gap-1">
                <input
                  type="checkbox"
                  checked={value.values.includes(priority)}
                  onChange={(e) =>
                    onChange({
                      ...value,
                      values: e.target.checked
                        ? [...value.values, priority]
                        : value.values.filter((v) => v !== priority),
                    })
                  }
                />
                {t(priorityLabels[priority])}
              </label>
            ))}
          </div>
        </fieldset>
      )}
      {value.kind === "number" && (
        <IntegerInput
          value={value.number}
          raw={numericInputs[path] ?? String(value.number)}
          onRaw={(raw) => onNumericInput(path, raw)}
          onChange={(number) => onChange({ ...value, number })}
        />
      )}
      {value.kind === "date" && (
        <div className="grid gap-1">
          <label htmlFor={id}>{t("searchviews.value")}</label>
          <DatePicker
            id={id}
            value={value.value}
            onChange={(next) => onChange({ ...value, value: next })}
          />
        </div>
      )}
    </fieldset>
  );
}
