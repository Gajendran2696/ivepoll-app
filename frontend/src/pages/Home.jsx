import { Link, Navigate } from 'react-router-dom'
import { useAuth } from '../auth.jsx'

// The hero is a small, static picture of the product's main moment: bars filling as votes arrive.
function DemoBars() {
  const rows = [
    ['Tabs', 46],
    ['Spaces', 31],
    ['Whatever the linter says', 23],
  ]
  return (
    <div className="demo" aria-hidden="true">
      <p className="demo-q">Tabs or spaces?</p>
      {rows.map(([label, pct], i) => (
        <div className="demo-row" key={label}>
          <div className="demo-fill" style={{ width: `${pct}%`, animationDelay: `${i * 140}ms` }} />
          <span>{label}</span>
          <strong>{pct}%</strong>
        </div>
      ))}
    </div>
  )
}

export default function Home() {
  const { user, ready } = useAuth()
  if (ready && user) return <Navigate to="/dashboard" replace />
  return (
    <section className="hero">
      <div className="hero-copy">
        <h1>Ask a room a question. Watch the answers arrive.</h1>
        <p className="lede">
          Create a poll, share the link, and everyone sees the results move the moment a vote comes in. No refresh,
          no app to install.
        </p>
        <div className="row">
          <Link to="/signup" className="btn btn-primary">Create your first poll</Link>
          <Link to="/login" className="btn btn-ghost">Log in</Link>
        </div>
        <p className="muted small">Voting is open to anyone with the link. You only need an account to create polls.</p>
      </div>
      <DemoBars />
    </section>
  )
}
