import { useState } from 'react'
import { Link, Navigate, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth.jsx'

export default function AuthPage({ mode }) {
  const isSignup = mode === 'signup'
  const { user, login, signup } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const dest = location.state?.from || '/dashboard'

  const [form, setForm] = useState({ name: '', email: '', password: '' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (user) return <Navigate to={dest} replace />

  const set = (k) => (e) => setForm((f) => ({ ...f, [k]: e.target.value }))

  // These checks only save a round trip; the server validates everything again.
  function validate() {
    if (isSignup && form.name.trim().length < 2) return 'Enter your name (at least 2 characters).'
    if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(form.email.trim())) return 'Enter a valid email address.'
    if (isSignup && form.password.length < 8) return 'Use a password with at least 8 characters.'
    if (!form.password) return 'Enter your password.'
    return ''
  }

  async function onSubmit(e) {
    e.preventDefault()
    const problem = validate()
    if (problem) return setError(problem)
    setError('')
    setBusy(true)
    try {
      if (isSignup) await signup(form.name, form.email, form.password)
      else await login(form.email, form.password)
      navigate(dest, { replace: true })
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="narrow">
      <h1>{isSignup ? 'Create your account' : 'Welcome back'}</h1>
      <p className="muted">
        {isSignup ? 'An account lets you create polls and manage them.' : 'Log in to manage your polls.'}
      </p>
      <form onSubmit={onSubmit} className="card form" noValidate>
        {isSignup && (
          <label>
            Name
            <input value={form.name} onChange={set('name')} autoComplete="name" maxLength={50} autoFocus />
          </label>
        )}
        <label>
          Email
          <input
            type="email"
            value={form.email}
            onChange={set('email')}
            autoComplete="email"
            autoFocus={!isSignup}
          />
        </label>
        <label>
          Password
          <input
            type="password"
            value={form.password}
            onChange={set('password')}
            autoComplete={isSignup ? 'new-password' : 'current-password'}
          />
          {isSignup && <span className="hint">At least 8 characters.</span>}
        </label>
        {error && <p className="error" role="alert">{error}</p>}
        <button className="btn btn-primary" disabled={busy}>
          {busy ? 'Please wait…' : isSignup ? 'Create account' : 'Log in'}
        </button>
      </form>
      <p className="muted center-text">
        {isSignup ? (
          <>Already have an account? <Link to="/login" state={location.state}>Log in</Link></>
        ) : (
          <>New here? <Link to="/signup" state={location.state}>Create an account</Link></>
        )}
      </p>
    </div>
  )
}
