import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "./ui";

type State = { error: Error | null };

/**
 * ErrorBoundary keeps one broken screen from blanking the whole app. React
 * unmounts everything when a render throws and nothing catches it; this
 * catches it, says what happened, and offers a way back.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Writers' Guild: a screen failed to render", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" className="mx-auto mt-16 max-w-md rounded-lg border border-red-200 bg-red-50 p-5 text-sm text-red-900 shadow-sm">
        <h1 className="mb-2 text-base font-semibold">This screen hit a problem</h1>
        <p className="mb-4 break-words text-red-800">{this.state.error.message}</p>
        <div className="flex flex-wrap gap-2">
          <Button variant="primary" onClick={() => window.location.reload()}>
            Reload
          </Button>
          <Button onClick={() => window.location.assign("/projects")}>Back to projects</Button>
        </div>
      </div>
    );
  }
}
