import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './auth.jsx'
import Home from './pages/Home.jsx'
import AuthPage from './pages/AuthPage.jsx'
import Dashboard from './pages/Dashboard.jsx'
import CreatePoll from './pages/CreatePoll.jsx'
import PollPage from './pages/PollPage.jsx'

function Logo() {
  return (
    <Link to="/" className="logo" aria-label="LivePoll home">
      <svg width="26" height="26" viewBox="0 0 32 32" aria-hidden="true">
        <rect width="32" height="32" rx="8" fill="var(--cobalt)" />
        <rect x="7" y="17" width="4" height="8" rx="1.5" fill="#fff" />
        <rect x="14" y="11" width="4" height="14" rx="1.5" fill="#fff" />
        <rect x="21" y="6" width="4" height="19" rx="1.5" fill="var(--amber)" />
      </svg>
      <span>LivePoll</span>
    </Link>
  )
}

function Header() {
  const { user, logout } = useAuth()
  return (
    <header className="topbar">
      <Logo />
      <nav>
        {user ? (
          <>
            <Link to="/dashboard">My polls</Link>
            <Link to="/new" className="btn btn-primary btn-sm">New poll</Link>
            <button className="linklike" onClick={logout}>Log out</button>
          </>
        ) : (
          <>
            <Link to="/login">Log in</Link>
            <Link to="/signup" className="btn btn-primary btn-sm">Sign up</Link>
          </>
        )}
      </nav>
    </header>
  )
}

// Only creating and managing polls needs an account; viewing and voting stay open to everyone.
function RequireAuth({ children }) {
  const { user, ready } = useAuth()
  const location = useLocation()
  if (!ready) return <div className="center muted">Loading…</div>
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return children
}

export default function App() {
  return (
    <>
      <Header />
      <main className="container">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/login" element={<AuthPage mode="login" />} />
          <Route path="/signup" element={<AuthPage mode="signup" />} />
          <Route path="/dashboard" element={<RequireAuth><Dashboard /></RequireAuth>} />
          <Route path="/new" element={<RequireAuth><CreatePoll /></RequireAuth>} />
          <Route path="/p/:slug" element={<PollPage />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
    </>
  )
}

function NotFound() {
  return (
    <div className="center">
      <h1>Page not found</h1>
      <p className="muted">That link doesn’t lead anywhere. Check the address or head back home.</p>
      <Link to="/" className="btn btn-primary">Go home</Link>
    </div>
  )
}
