import { useEffect } from "react";

function configuredValue(value: string | undefined): string | null {
  const normalized = value?.trim();
  if (!normalized || /^(undefined|null|false)$/i.test(normalized) || normalized.includes("%VITE_")) {
    return null;
  }
  return normalized;
}

export default function AnalyticsTracker() {
  useEffect(() => {
    const endpoint = configuredValue(import.meta.env.VITE_ANALYTICS_ENDPOINT);
    const websiteId = configuredValue(import.meta.env.VITE_ANALYTICS_WEBSITE_ID);

    if (!endpoint || !websiteId) return;

    let src: string;
    try {
      const url = new URL(endpoint);
      if (url.protocol !== "https:" && url.protocol !== "http:") return;
      src = `${url.toString().replace(/\/$/, "")}/umami`;
    } catch {
      return;
    }

    if (document.querySelector('script[data-campaign-analytics="true"]')) return;

    const script = document.createElement("script");
    script.dataset.campaignAnalytics = "true";
    script.src = src;
    script.defer = true;
    script.dataset.websiteId = websiteId;
    script.onerror = () => script.remove();
    document.head.appendChild(script);
  }, []);

  return null;
}
