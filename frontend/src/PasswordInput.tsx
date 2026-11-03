import { useState, type InputHTMLAttributes } from 'react';
import { EyeIcon, EyeOffIcon } from './icons';

// Password field with a show/hide toggle. Takes the same props as <input>.
export default function PasswordInput({ className = 'input', ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'>) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="password-wrap">
      <input {...props} className={className + ' password-input'} type={visible ? 'text' : 'password'} />
      <button type="button" className="password-toggle" onClick={() => setVisible((v) => !v)}
        aria-label={visible ? 'Hide password' : 'Show password'} aria-pressed={visible}>
        {visible ? <EyeOffIcon /> : <EyeIcon />}
      </button>
    </div>
  );
}
