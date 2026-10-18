"use client";
import type { ComponentProps } from "react";
import { Input } from "./input";
import { Textarea } from "./textarea";
import { useLocale } from "@/providers/locale-provider";
import { withinCodePointLimit, withinByteLimit } from "@/lib/text-limits.mjs";
type Limits = { maxCodePoints?: number; maxBytes?: number };
function useLimitMessage({ maxCodePoints, maxBytes }: Limits) {
  const { t } = useLocale();
  return (value: string) =>
    maxCodePoints !== undefined &&
    !withinCodePointLimit(value.trim(), maxCodePoints)
      ? t("common.max_characters", { limit: maxCodePoints })
      : maxBytes !== undefined && !withinByteLimit(value.trim(), maxBytes)
        ? t("common.max_bytes", { limit: maxBytes })
        : "";
}
// Validate without truncating UTF-16 surrogate pairs as native maxLength does.
export function TextLimitInput({
  maxCodePoints,
  maxBytes,
  onChange,
  ...props
}: ComponentProps<typeof Input> & Limits) {
  const message = useLimitMessage({ maxCodePoints, maxBytes });
  return (
    <Input
      {...props}
      aria-invalid={
        message(String(props.value ?? "")) ? true : props["aria-invalid"]
      }
      ref={(node) => {
        if (node) node.setCustomValidity(message(node.value));
      }}
      onChange={(event) => {
        event.currentTarget.setCustomValidity(
          message(event.currentTarget.value),
        );
        onChange?.(event);
      }}
    />
  );
}
export function TextLimitTextarea({
  maxCodePoints,
  maxBytes,
  onChange,
  ...props
}: ComponentProps<typeof Textarea> & Limits) {
  const message = useLimitMessage({ maxCodePoints, maxBytes });
  return (
    <Textarea
      {...props}
      aria-invalid={
        message(String(props.value ?? "")) ? true : props["aria-invalid"]
      }
      ref={(node) => {
        if (node) node.setCustomValidity(message(node.value));
      }}
      onChange={(event) => {
        event.currentTarget.setCustomValidity(
          message(event.currentTarget.value),
        );
        onChange?.(event);
      }}
    />
  );
}
