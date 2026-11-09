const flow = [
  {
    who: 'Developer',
    act: 'requests',
    body: 'Writes an UPDATE or DELETE against one governed database and says why.',
  },
  {
    who: 'Approver',
    act: 'reads',
    body: 'Sees exactly what it would touch and how many rows — before anything changes.',
  },
  {
    who: 'Avior',
    act: 'runs it',
    body: 'Executes the write and keeps what it overwrote, in the same transaction.',
  },
] as const

export function Writes() {
  return (
    <section id="writes" className="flood">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow eyebrow-on-flood">Governed writes</p>
          <h2 className="section-title">A change to production needs a second person.</h2>
          <p className="section-lede">
            Reads are self-serve. Writes are requested, read by somebody else, and then run — never typed straight
            into a console.
          </p>
        </div>
        <ol className="flow">
          {flow.map((f, i) => (
            <li key={f.who} className="flow-card">
              <span className="flow-n">{i + 1}</span>
              <p className="flow-who">
                {f.who} <span className="flow-act">{f.act}</span>
              </p>
              <p className="flow-body">{f.body}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}
