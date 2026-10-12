import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'

const MIN = 2
const MAX = 10

export default function CreatePoll() {
  const navigate = useNavigate()
  const [question, setQuestion] = useState('')
  const [options, setOptions] = useState(['', ''])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const setOpt = (i, v) => setOptions((o) => o.map((x, j) => (j === i ? v : x)))
  const addOpt = () => setOptions((o) => (o.length < MAX ? [...o, ''] : o))
  const removeOpt = (i) => setOptions((o) => (o.length > MIN ? o.filter((_, j) => j !== i) : o))

  async function onSubmit(e) {
    e.preventDefault()
    const filled = options.map((o) => o.trim()).filter(Boolean)
    if (question.trim().length < 3) return setError('Write a question of at least 3 characters.')
    if (filled.length < MIN) return setError('Add at least two answer options.')
    if (new Set(filled.map((o) => o.toLowerCase())).size !== filled.length)
      return setError('Each option must be different.')
    setError('')
    setBusy(true)
    try {
      const poll = await api.createPoll({ question, options: filled })
      navigate(`/p/${poll.slug}`, { replace: true })
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  return (
    <div className="narrow">
      <h1>New poll</h1>
      <form onSubmit={onSubmit} className="card form" noValidate>
        <label>
          Question
          <input
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            maxLength={200}
            placeholder="What should we build next?"
            autoFocus
          />
        </label>

        <fieldset>
          <legend>Answer options</legend>
          {options.map((o, i) => (
            <div className="opt-row" key={i}>
              <input
                value={o}
                onChange={(e) => setOpt(i, e.target.value)}
                maxLength={100}
                placeholder={`Option ${i + 1}`}
                aria-label={`Option ${i + 1}`}
              />
              <button
                type="button"
                className="icon-btn"
                onClick={() => removeOpt(i)}
                disabled={options.length <= MIN}
                aria-label={`Remove option ${i + 1}`}
              >
                ✕
              </button>
            </div>
          ))}
          <button type="button" className="btn btn-ghost btn-sm" onClick={addOpt} disabled={options.length >= MAX}>
            Add option
          </button>
          {options.length >= MAX && <span className="hint"> Maximum {MAX} options.</span>}
        </fieldset>

        {error && <p className="error" role="alert">{error}</p>}
        <button className="btn btn-primary" disabled={busy}>
          {busy ? 'Creating…' : 'Create poll'}
        </button>
      </form>
    </div>
  )
}
