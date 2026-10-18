"use client";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { drawerDrafts } from "./drafts";
export const DrawerDraftContext = createContext<string | null>(null);
// Outside the drawer this remains ordinary component-local state. Only active
// editor trees mount queries; reopening restores the original CAS baseline.
export function useDrawerDraft<T>(
  slot: string,
  initial: T | (() => T),
  explicitScope?: string,
): [T, Dispatch<SetStateAction<T>>] {
  const inheritedScope = useContext(DrawerDraftContext);
  const scope = explicitScope ?? inheritedScope;
  const [value, setValue] = useState<T>(() => {
    const seed = () =>
      typeof initial === "function" ? (initial as () => T)() : initial;
    return scope ? drawerDrafts.read(scope, slot, seed) : seed();
  });
  const latest = useRef(value),
    alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const update: Dispatch<SetStateAction<T>> = (next) => {
    if (!alive.current) return;
    const resolved =
      typeof next === "function"
        ? (next as (prior: T) => T)(latest.current)
        : next;
    latest.current = resolved;
    if (scope) drawerDrafts.write(scope, slot, resolved);
    setValue(resolved);
  };
  return [value, update];
}
