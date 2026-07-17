import { Component, ErrorInfo, ReactNode } from 'react';
import { logger } from '@/lib/utils';

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
  onError?: (error: Error, info: ErrorInfo) => void;
  resetKeys?: unknown[];
}

interface State {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
  retryCount: number;
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null, errorInfo: null, retryCount: 0 };
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    this.setState({ errorInfo });
    logger.error('[ErrorBoundary]', error, errorInfo);
    if (this.props.onError) this.props.onError(error, errorInfo);
    try {
      fetch('/api/v1/errors/frontend', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: error.message,
          stack: error.stack,
          componentStack: errorInfo.componentStack,
          url: window.location.href,
          timestamp: new Date().toISOString(),
        }),
      }).catch(() => {});
    } catch {}
  }

  componentDidUpdate(prevProps: Props) {
    if (
      this.state.hasError &&
      prevProps.resetKeys &&
      this.props.resetKeys &&
      prevProps.resetKeys.some((key, i) => key !== this.props.resetKeys![i])
    ) {
      this.setState({ hasError: false, error: null, errorInfo: null });
    }
  }

  handleRetry = () => {
    this.setState(s => ({
      hasError: false,
      error: null,
      errorInfo: null,
      retryCount: s.retryCount + 1,
    }));
  };

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback;
      return (
        <div role="alert" aria-live="assertive" className="flex items-center justify-center min-h-[400px] p-8">
          <div className="max-w-lg w-full bg-red-50 dark:bg-red-950 border border-red-200 dark:border-red-800 rounded-lg p-6 shadow-lg">
            <div className="flex items-center gap-3 mb-3">
              <span aria-hidden="true" className="text-2xl">⚠️</span>
              <h2 className="text-lg font-semibold text-red-800 dark:text-red-200">
                An unexpected error occurred
              </h2>
            </div>
            <p className="text-sm text-red-600 dark:text-red-400 mb-4">
              {this.state.error?.message || 'The application encountered an error. Please try again.'}
            </p>
            <div className="flex gap-3">
              <button
                onClick={this.handleRetry}
                className="px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2 text-sm font-medium"
                aria-label="Retry the failed operation"
              >
                Try Again
              </button>
              <button
                onClick={() => window.location.reload()}
                className="px-4 py-2 bg-white dark:bg-gray-800 text-red-700 dark:text-red-300 border border-red-300 dark:border-red-700 rounded hover:bg-red-50 dark:hover:bg-red-900 focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2 text-sm font-medium"
                aria-label="Reload the page"
              >
                Reload Page
              </button>
            </div>
            {this.state.retryCount > 2 && (
              <p className="mt-3 text-xs text-red-500 dark:text-red-400">
                Persistent errors? Please contact INEC technical support.
              </p>
            )}
            {import.meta.env.DEV && this.state.errorInfo && (
              <details className="mt-4">
                <summary className="text-xs text-red-500 cursor-pointer select-none">
                  Developer: Show stack trace
                </summary>
                <pre className="text-xs mt-2 p-2 bg-red-100 dark:bg-red-900 rounded overflow-auto max-h-48 whitespace-pre-wrap">
                  {this.state.error?.stack}
                  {'\n\nComponent Stack:'}
                  {this.state.errorInfo.componentStack}
                </pre>
              </details>
            )}
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

export default ErrorBoundary;
