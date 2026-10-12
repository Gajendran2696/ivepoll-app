import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useAuth } from '../auth.jsx'

export default function Dashboard() {
  const { user } = useAuth()
  const [polls, setPolls] = useState(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    api
      .myPolls()
      .then((r) => alive && setPolls(r.polls))
      .catch((e) => alive && setError(e.message))
    return () => {
      alive = false
    }
  }, [])

  return (
    <div className="narrow wide">
      <div className="between">
        <h1>Hi {user.name.split(' ')[0]}, here are your polls</h1>
        <Link to="/new" className="btn btn-primary">New poll</Link>
      </div>

      {error && <p className="error" role="alert">{error}</p>}
      {!polls && !error && <p className="muted">Loading your polls…</p>}

      {polls && polls.length === 0 && (
        <div className="card empty">
          <h2>No polls yet</h2>
          <p className="muted">Create one, share the link, and watch votes come in.</p>
          <Link to="/new" className="btn btn-primary">Create your first poll</Link>
        </div>
      )}

      {polls && polls.length > 0 && (
        <ul className="poll-list">
          {polls.map((p) => (
            <li key={p.slug}>
              <Link to={`/p/${p.slug}`} className="card poll-item">
                <span className="poll-item-q">{p.question}</span>
                <span className="poll-item-meta">
                  <span className={`chip ${p.closed ? 'chip-closed' : 'chip-open'}`}>{p.closed ? 'Closed' : 'Open'}</span>
                  <span className="muted">
                    {p.total} {p.total === 1 ? 'vote' : 'votes'}
                  </span>
                  <span className="muted">{new Date(p.createdAt).toLocaleDateString()}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
