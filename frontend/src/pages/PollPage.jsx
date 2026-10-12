import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { useLivePoll } from '../useLivePoll'
import ShareCard from '../components/ShareCard.jsx'

function ConnectionPill({ state }) {
  const label = { live: 'Live', connecting: 'Connecting…', reconnecting: 'Reconnecting…' }[state]
  return (
    <span className={`pill pill-${state}`} role="status">
      <i aria-hidden="true" />
      {label}
    </span>
  )
}

export default function PollPage() {
  const { slug } = useParams()
  const navigate = useNavigate()
  const live = useLivePoll(slug)
  const { poll, setPoll, counts, total, viewers, closed, setClosed, deleted, connection, lastBumped } = live

  const [selected, setSelected] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  if (live.loadError) {
    return (
      <div className="center">
        <h1>{live.loadError.status === 404 ? 'Poll not found' : 'Couldn’t load this poll'}</h1>
        <p className="muted">
          {live.loadError.status === 404
            ? 'The link may be mistyped, or the poll was deleted.'
            : live.loadError.message}
        </p>
        <Link to="/" className="btn btn-primary">Go home</Link>
      </div>
    )
  }
  if (!poll) return <div className="center muted">Loading poll…</div>
  if (deleted) {
    return (
      <div className="center">
        <h1>This poll was deleted</h1>
        <p className="muted">Its owner removed it, so there are no more results to show.</p>
        <Link to="/" className="btn btn-primary">Go home</Link>
      </div>
    )
  }

  const votedId = poll.votedOptionId
  const canVote = !closed && !votedId
  const leaderCount = Math.max(0, ...Object.values(counts))

  async function castVote() {
    if (!selected) return
    setBusy(true)
    setError('')
    try {
      const r = await api.vote(slug, selected)
      live.applyResults(r.counts, r.total)
      setPoll((p) => ({ ...p, votedOptionId: r.votedOptionId }))
    } catch (err) {
      setError(err.message)
      if (err.status === 409) {
        // Already voted (maybe in another tab) or poll closed: re-sync with the server.
        api.getPoll(slug).then((p) => {
          setPoll(p)
          setClosed(p.closed)
        }).catch(() => {})
      }
    } finally {
      setBusy(false)
    }
  }

  async function toggleClosed() {
    setError('')
    try {
      const r = await api.setClosed(slug, !closed)
      setClosed(r.closed)
    } catch (err) {
      setError(err.message)
    }
  }

  async function remove() {
    if (!window.confirm('Delete this poll and all its votes? This cannot be undone.')) return
    try {
      await api.deletePoll(slug)
      navigate('/dashboard', { replace: true })
    } catch (err) {
      setError(err.message)
    }
  }

  return (
    <div className="narrow wide">
      <div className="between meta">
        <ConnectionPill state={connection} />
        <span className="muted" aria-live="polite">
          {viewers > 0 ? `${viewers} watching now` : ''}
        </span>
      </div>

      <h1 className="question">{poll.question}</h1>

      <ul className="options" aria-label="Answer options">
        {poll.options.map((o) => {
          const n = counts[o.id] ?? 0
          const pct = total ? Math.round((n / total) * 100) : 0
          const mine = votedId === o.id
          const bumped = lastBumped?.id === o.id
          return (
            <li key={o.id}>
              <button
                type="button"
                className={[
                  'option',
                  canVote ? 'selectable' : '',
                  selected === o.id && canVote ? 'selected' : '',
                  mine ? 'mine' : '',
                  n > 0 && n === leaderCount ? 'leader' : '',
                ].join(' ')}
                disabled={!canVote}
                aria-pressed={canVote ? selected === o.id : undefined}
                onClick={() => setSelected(o.id)}
              >
                <span className="bar" style={{ width: `${pct}%` }} />
                {bumped && <span key={lastBumped.at} className="flash" aria-hidden="true" />}
                <span className="label">
                  {o.text}
                  {mine && <span className="chip chip-open">Your vote</span>}
                </span>
                <span className="num">
                  <b>{pct}%</b>
                  <small>{n}</small>
                </span>
              </button>
            </li>
          )
        })}
      </ul>

      <div className="between footer-row">
        <span className="total" aria-live="polite">
          {total} {total === 1 ? 'vote' : 'votes'}
        </span>
        {canVote && (
          <button className="btn btn-primary" disabled={!selected || busy} onClick={castVote}>
            {busy ? 'Sending…' : 'Cast vote'}
          </button>
        )}
      </div>

      {closed && <p className="notice">Voting is closed. These are the final results.</p>}
      {votedId && !closed && <p className="notice ok">Thanks, your vote is in. Results update as others vote.</p>}
      {error && <p className="error" role="alert">{error}</p>}

      {poll.isOwner ? (
        <>
          <ShareCard slug={slug} />
          <div className="row owner-actions">
            <button className="btn btn-ghost" onClick={toggleClosed}>
              {closed ? 'Reopen voting' : 'Close voting'}
            </button>
            <button className="btn btn-danger" onClick={remove}>Delete poll</button>
          </div>
        </>
      ) : (
        <p className="muted small">
          Want to ask your own question? <Link to="/signup">Create a poll</Link>
        </p>
      )}
    </div>
  )
}
