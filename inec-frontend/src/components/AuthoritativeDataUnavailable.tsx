import { AlertTriangle, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';

interface AuthoritativeDataUnavailableProps {
  title?: string;
  description?: string;
  error?: string | null;
  onRetry?: () => void;
  className?: string;
}

/**
 * Use when an operational page cannot obtain authoritative server data.
 * This component deliberately contains no local election or operational fallback
 * records: a connectivity failure must never be represented as plausible live data.
 */
export function AuthoritativeDataUnavailable({
  title = 'Authoritative data is unavailable',
  description = 'This view is withheld until the platform can retrieve verified records from its configured source.',
  error,
  onRetry,
  className = '',
}: AuthoritativeDataUnavailableProps) {
  return (
    <Card className={`border-amber-300 bg-amber-50/70 dark:border-amber-700 dark:bg-amber-950/30 ${className}`}>
      <CardContent className="flex min-h-56 flex-col items-center justify-center gap-4 px-6 py-8 text-center">
        <div className="rounded-full bg-amber-100 p-3 text-amber-800 dark:bg-amber-900/60 dark:text-amber-200">
          <AlertTriangle className="h-6 w-6" aria-hidden="true" />
        </div>
        <div className="max-w-xl space-y-2">
          <h2 className="text-lg font-semibold text-amber-950 dark:text-amber-100">{title}</h2>
          <p className="text-sm text-amber-900/85 dark:text-amber-200/85">{description}</p>
          {error ? <p className="text-xs text-amber-800/80 dark:text-amber-300/80">Reference: {error}</p> : null}
        </div>
        {onRetry ? (
          <Button type="button" variant="outline" onClick={onRetry} className="border-amber-400 bg-white text-amber-950 hover:bg-amber-100 dark:bg-transparent dark:text-amber-100 dark:hover:bg-amber-900/50">
            <RefreshCw className="mr-2 h-4 w-4" aria-hidden="true" />
            Retry authoritative source
          </Button>
        ) : null}
      </CardContent>
    </Card>
  );
}
