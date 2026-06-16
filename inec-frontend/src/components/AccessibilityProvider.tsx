/**
 * Accessibility and responsive design enhancements.
 * - Skip navigation link for keyboard users
 * - Focus management
 * - Screen reader announcements
 * - Reduced motion support
 * - Touch-friendly target sizes on mobile
 */
import { createContext, useContext, useEffect, useState, ReactNode } from 'react';

interface A11yContext {
  prefersReducedMotion: boolean;
  isMobile: boolean;
  isTablet: boolean;
  announce: (message: string) => void;
}

const A11yCtx = createContext<A11yContext>({
  prefersReducedMotion: false,
  isMobile: false,
  isTablet: false,
  announce: () => {},
});

export function useA11y() {
  return useContext(A11yCtx);
}

export function AccessibilityProvider({ children }: { children: ReactNode }) {
  const [prefersReducedMotion, setPrefersReducedMotion] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [isTablet, setIsTablet] = useState(false);

  useEffect(() => {
    const motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
    setPrefersReducedMotion(motionQuery.matches);
    const handler = (e: MediaQueryListEvent) => setPrefersReducedMotion(e.matches);
    motionQuery.addEventListener('change', handler);

    const checkSize = () => {
      setIsMobile(window.innerWidth < 640);
      setIsTablet(window.innerWidth >= 640 && window.innerWidth < 1024);
    };
    checkSize();
    window.addEventListener('resize', checkSize);

    return () => {
      motionQuery.removeEventListener('change', handler);
      window.removeEventListener('resize', checkSize);
    };
  }, []);

  const announce = (message: string) => {
    const el = document.getElementById('a11y-announcer');
    if (el) {
      el.textContent = '';
      requestAnimationFrame(() => { el.textContent = message; });
    }
  };

  return (
    <A11yCtx.Provider value={{ prefersReducedMotion, isMobile, isTablet, announce }}>
      {/* Skip to main content link */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[9999] focus:bg-green-700 focus:text-white focus:px-4 focus:py-2 focus:rounded-lg focus:text-sm"
      >
        Skip to main content
      </a>

      {/* Screen reader live region */}
      <div id="a11y-announcer" aria-live="polite" aria-atomic="true" className="sr-only" role="status" />

      {children}
    </A11yCtx.Provider>
  );
}
