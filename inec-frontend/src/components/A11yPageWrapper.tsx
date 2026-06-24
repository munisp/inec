import { useEffect, useRef } from 'react';

interface A11yPageWrapperProps {
  title: string;
  children: React.ReactNode;
  /** Optional live region text announced on page load */
  announceOnLoad?: string;
}

/**
 * Wraps page content with:
 * - <section role="region"> with aria-label
 * - Focus management on mount (moves focus to heading)
 * - Live region for dynamic announcements
 */
export function A11yPageWrapper({ title, children, announceOnLoad }: A11yPageWrapperProps) {
  const headingRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    headingRef.current?.focus();
  }, [title]);

  return (
    <section role="region" aria-label={title}>
      <h1 ref={headingRef} tabIndex={-1} className="sr-only">{title}</h1>
      {announceOnLoad && (
        <div role="status" aria-live="polite" className="sr-only">
          {announceOnLoad}
        </div>
      )}
      {children}
    </section>
  );
}

/** Loading state with aria-busy and live region */
export function A11yLoading({ message = 'Loading data...' }: { message?: string }) {
  return (
    <div role="status" aria-busy="true" aria-live="polite" className="flex items-center justify-center py-12">
      <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-green-700" aria-hidden="true" />
      <span className="ml-3 text-zinc-600 dark:text-zinc-400">{message}</span>
    </div>
  );
}

/** Error state with alert role */
export function A11yError({ message }: { message: string }) {
  return (
    <div role="alert" className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4 text-red-700 dark:text-red-400">
      {message}
    </div>
  );
}
