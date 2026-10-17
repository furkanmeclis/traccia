"use client";
import type { ReactNode } from "react";
import Link from "next/link";
import { Layers3 } from "lucide-react";
import { ImpersonationBanner } from "@/features/auth/components/impersonation-banner";
import { RouteGuard } from "@/components/common/route-guard";
import { LocaleSwitch } from "@/components/layout/locale-switch";
import { ThemeSwitch } from "@/components/layout/theme-switch";
import { UserMenu } from "@/components/layout/user-menu";
import { useLocale } from "@/providers/locale-provider";
export function ProductShell({ children }: { children: ReactNode }) {
  const { t } = useLocale();
  return (
    <RouteGuard mode="product">
      <div className="bg-background min-h-svh">
        <header className="bg-background/95 sticky top-0 z-30 border-b backdrop-blur">
          <div className="mx-auto flex h-16 max-w-6xl items-center gap-3 px-4 sm:px-8">
            <Link
              href="/workspaces"
              className="flex items-center gap-2 text-lg font-semibold tracking-tight"
            >
              <Layers3 className="text-primary size-5" />
              Traccia
            </Link>
            <Link
              href="/workspaces"
              className="text-muted-foreground hover:text-foreground ml-2 text-sm max-sm:sr-only"
            >
              {t("workspaces.title")}
            </Link>
            <div className="ml-auto flex items-center gap-1">
              <LocaleSwitch />
              <ThemeSwitch />
              <UserMenu />
            </div>
          </div>
        </header>
        <div className="bg-background sticky top-16 z-20 mx-auto max-w-6xl px-4 sm:px-8">
          <ImpersonationBanner />
        </div>
        <main className="mx-auto max-w-6xl px-4 py-8 sm:px-8 sm:py-12">
          {children}
        </main>
      </div>
    </RouteGuard>
  );
}
