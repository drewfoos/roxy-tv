import { Component, type ErrorInfo, type ReactNode } from 'react'

/**
 * Catches a page that throws while rendering. Without it React unmounts everything and
 * the site goes blank; with it the page shows what went wrong and the way home.
 */
export class PageErrorBoundary extends Component<{ children: ReactNode; resetKey?: string }, { error: Error | null }> {
  state: { error: Error | null } = { error: null }

  // The boundary is kept across some path changes (episode to episode on the watch page):
  // an error shown for one path must not stay for the next.
  componentDidUpdate(prev: { resetKey?: string }) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: null })
  }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('page crashed', error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="page-error">
        <h1>This page hit a problem</h1>
        <p className="notice notice--error">{this.state.error.message || 'Something went wrong.'}</p>
        <div className="page-error__actions">
          <button type="button" className="btn btn--brand" onClick={() => window.location.reload()}>
            Reload
          </button>
          <a className="btn btn--grey" href="/">
            Go home
          </a>
        </div>
      </div>
    )
  }
}
