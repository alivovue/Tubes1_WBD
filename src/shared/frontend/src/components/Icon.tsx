import { useId, type ReactNode } from "react";

/** Small, locally drawn glass objects. IDs stay unique when an icon repeats. */
export function Icon({ name }: { name: string }) {
  const id = `aero-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const paint = (color: string) => `url(#${id}-${color})`;
  const blue = paint("blue");
  const green = paint("green");
  const silver = paint("silver");
  const gold = paint("gold");
  const shine = paint("shine");
  const stroke = { fill: "none", stroke: blue, strokeWidth: 6, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
  let object: ReactNode;

  switch (name) {
    case "users":
      object = <>
        <ellipse cx="33" cy="60" rx="27" ry="3" fill="#176d89" opacity=".18" />
        <path d="M21 26C11 26 3 38 3 47c0 12 36 12 36 0 0-9-8-21-18-21Z" fill={green} stroke="#148132" strokeWidth="1" />
        <path d="M11 35c-4 5-6 12-4 15 2 3 4 3 6 3-3-6-2-12 1-18Z" fill="#004d2e" opacity=".35" />
        <circle cx="21" cy="16" r="11" fill={green} stroke="#168432" strokeWidth="1" />
        <ellipse cx="19" cy="11" rx="8" ry="5" fill={shine} />
        <path d="M13 23c4 3 11 3 15-1" fill="none" stroke="#ccffac" strokeWidth="1.5" opacity=".6" />
        <path d="M42 35c-13 0-25 15-25 23 0 10 46 10 46-1 0-9-10-22-21-22Z" fill={blue} stroke="#075d9f" strokeWidth="1.2" />
        <path d="M29 41c-7 6-12 15-8 18l7 2c-5-7-2-14 4-20Zm24 1c8 7 11 14 7 17l-5 2c3-7 1-13-4-19Z" fill="#003b86" opacity=".4" />
        <path d="M31 42c3-5 11-7 16-3 4 3 6 7 7 13-7-6-15-8-25-6Z" fill={shine} opacity=".55" />
        <path d="M26 60c9 4 22 4 30-1" fill="none" stroke="#b9faff" strokeWidth="2" opacity=".7" />
        <circle cx="42" cy="23" r="13" fill={blue} stroke="#075d9f" strokeWidth="1.2" />
        <ellipse cx="39" cy="16.5" rx="9.5" ry="5.7" fill={shine} />
        <path d="M32 30c4 5 13 6 19 0" fill="none" stroke="#b5f7ff" strokeWidth="1.8" opacity=".65" />
      </>;
      break;
    case "database":
      object = <>
        <ellipse cx="32" cy="57" rx="24" ry="4" fill="#166c8e" opacity=".2" />
        <path d="M9 15v34c0 14 46 14 46 0V15Z" fill={blue} stroke="#0870a3" strokeWidth="1" />
        <path d="M10 27c0 11 44 11 44 0M10 39c0 11 44 11 44 0" fill="none" stroke="#0b6497" strokeWidth="2" />
        <path d="M11 29c4 8 38 9 42 0M11 41c4 8 38 9 42 0" fill="none" stroke="#b4f7ff" strokeWidth="1.4" opacity=".8" />
        <ellipse cx="32" cy="15" rx="23" ry="9" fill={silver} stroke="#1586b5" strokeWidth="1" />
        <ellipse cx="32" cy="14" rx="17" ry="5.5" fill={blue} />
        <path d="M15 24v24c0 3 3 5 6 5V26Z" fill={shine} opacity=".7" />
      </>;
      break;
    case "grid":
      object = <>{[[7, 7, blue], [35, 7, green], [7, 35, green], [35, 35, blue]].map(([x, y, fill], i) => <g key={i}>
        <rect x={x} y={Number(y) + 2} width="23" height="23" rx="6" fill="#126b91" opacity=".4" />
        <rect x={x} y={y} width="23" height="23" rx="6" fill={String(fill)} stroke="#fff" strokeOpacity=".5" />
        <rect x={Number(x) + 3} y={Number(y) + 2} width="17" height="9" rx="4" fill={shine} />
      </g>)}</>;
      break;
    case "code":
    case "table":
      object = <>
        <rect x="5" y="9" width="54" height="45" rx="8" fill={silver} stroke="#609bb3" strokeWidth="1" />
        <rect x="8" y="12" width="48" height="38" rx="5" fill={blue} />
        <path d="M11 14h42v12c-12-5-28-5-42 1Z" fill={shine} />
        {name === "code" ? <path d="m24 27-8 6 8 6m16-12 8 6-8 6m-6-15-5 23" fill="none" stroke="#f0ffff" strokeWidth="3.4" /> : <g stroke="#eaffff" strokeWidth="2.2" fill="none"><path d="M14 23h36v21H14ZM14 30h36M25 23v21M38 23v21M14 37h36" /></g>}
        <path d="M12 53h40" stroke="#3e7e9a" strokeWidth="2" />
      </>;
      break;
    case "history":
      object = <>
        <circle cx="33" cy="32" r="24" fill={blue} stroke="#0873a3" />
        <circle cx="33" cy="32" r="18" fill={silver} />
        <path d="M33 20v13l9 5" fill="none" stroke="#146e95" strokeWidth="3.5" />
        <ellipse cx="29" cy="16" rx="16" ry="7" fill={shine} />
        <path d="M12 25A24 24 0 0 1 51 12" fill="none" stroke={green} strokeWidth="7" />
        <path d="m7 12 3 17 16-6Z" fill={green} stroke="#1b8742" />
      </>;
      break;
    case "layers":
      object = <>
        <path d="m5 40 27-12 27 12-27 17Z" fill={blue} stroke="#166d99" />
        <path d="m5 28 27-12 27 12-27 17Z" fill={green} stroke="#168654" />
        <path d="m5 16 27-12 27 12-27 17Z" fill={silver} stroke="#6eabc4" />
        <path d="m8 16 24-10 24 10-24 9Z" fill={shine} />
        <path d="m8 29 24 14 24-14M8 41l24 14 24-14" stroke="#d7ffff" strokeWidth="1.5" fill="none" opacity=".8" />
      </>;
      break;
    case "server":
      object = <>
        <rect x="8" y="7" width="48" height="49" rx="7" fill={blue} stroke="#156a94" />
        {[13, 34].map(y => <g key={y}><rect x="11" y={y} width="42" height="18" rx="4" fill={silver} stroke="#7baebf" /><path d={`M17 ${y+6}h17m-17 5h17`} stroke="#5d8799" strokeWidth="2" /><circle cx="44" cy={y+9} r="4" fill={green} /><path d={`M14 ${y+2}h36v5H14Z`} fill={shine} /></g>)}
      </>;
      break;
    case "shield":
      object = <>
        <path d="m32 4 24 9v18c0 17-24 29-24 29S8 48 8 31V13Z" fill={silver} stroke="#659caf" />
        <path d="m32 9 19 7v15c0 13-19 24-19 24S13 44 13 31V16Z" fill={green} />
        <path d="m16 18 16-6 16 6v13c-9-4-21-4-32 0Z" fill={shine} />
        <path d="m22 31 7 7 14-16" fill="none" stroke="#f2fff0" strokeWidth="5" />
      </>;
      break;
    case "bell":
      object = <><circle cx="32" cy="53" r="6" fill={gold} /><path d="M14 39V26c0-23 36-23 36 0v13l6 7c2 4-50 4-48 0Z" fill={gold} stroke="#a47812" /><path d="M20 24c-1-10 9-16 17-12 4 2 7 6 7 10-9-3-17-2-24 2Z" fill={shine} /><path d="M32 5v4" stroke="#bd881c" strokeWidth="4" /></>;
      break;
    case "key":
      object = <><path d="m28 34 25-25 5 5-4 4 3 4-7 7-4-3-11 12Z" fill={gold} stroke="#a77c1c" /><circle cx="21" cy="43" r="15" fill={gold} stroke="#a77c1c" /><circle cx="18" cy="46" r="6" fill="#d5f4f5" stroke="#9b7c37" /><path d="M10 39c2-9 13-13 21-5" fill="none" stroke="#fffbdc" strokeWidth="3" /></>;
      break;
    case "alert":
      object = <><path d="M27 7c2-4 8-4 10 0l25 45c2 4-1 8-5 8H7c-4 0-7-4-5-8Z" fill={gold} stroke="#ac7911" /><path d="m13 36 18-28 16 27c-11-5-24-5-34 1Z" fill={shine} /><path d="M32 23v16" stroke="#745115" strokeWidth="5" /><circle cx="32" cy="49" r="3" fill="#745115" /></>;
      break;
    case "search":
      object = <><path d="m41 41 16 16" stroke="#0b6499" strokeWidth="10" /><path d="m43 42 14 14" stroke={blue} strokeWidth="6" /><circle cx="27" cy="27" r="21" fill={silver} stroke="#598da5" /><circle cx="27" cy="27" r="15.5" fill={blue} fillOpacity=".65" /><ellipse cx="23" cy="21" rx="11" ry="7" fill={shine} /></>;
      break;
    case "eye":
      object = <><path d="M3 32S14 15 32 15s29 17 29 17-11 17-29 17S3 32 3 32Z" fill={silver} stroke="#5c9ab3" /><circle cx="32" cy="32" r="13" fill={blue} /><circle cx="32" cy="33" r="6" fill="#16455c" /><ellipse cx="28" cy="27" rx="5" ry="4" fill="#fff" opacity=".9" /></>;
      break;
    case "edit":
      object = <><path d="m12 44 30-34 13 12-31 33-16 5Z" fill={gold} stroke="#a57515" /><path d="m13 44 12 10-16 5Z" fill={silver} /><path d="m8 60 3-10 6 7Z" fill="#335f76" /><path d="m39 13 6-6c6-5 18 7 13 13l-4 5Z" fill={blue} /><path d="m20 44 24-27" stroke="#fff3ac" strokeWidth="3" /></>;
      break;
    case "chevron": object = <path {...stroke} d="m25 13 19 19-19 19" />; break;
    case "arrow": object = <path {...stroke} d="M9 32h44M36 15l17 17-17 17" />; break;
    case "plus": object = <path {...stroke} d="M32 10v44M10 32h44" />; break;
    case "check": object = <path {...stroke} stroke={green} strokeWidth="8" d="m10 32 14 14 30-31" />; break;
    case "menu": object = <path {...stroke} d="M10 15h44M10 32h44M10 49h44" />; break;
    case "close": object = <path {...stroke} d="m15 15 34 34M49 15 15 49" />; break;
    case "logout": object = <><path {...stroke} stroke={silver} strokeWidth="7" d="M35 9h17v46H35" /><path {...stroke} d="M8 32h32m-12-12 12 12-12 12" /></>; break;
    default: object = <circle cx="32" cy="32" r="24" fill={blue} />;
  }

  return (
    <svg className={`aero-icon aero-icon--${name}`} viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      <defs>
        <radialGradient id={`${id}-blue`} cx="30%" cy="22%" r="85%">
          <stop stopColor="#c4ffff" /><stop offset=".27" stopColor="#35e2ff" /><stop offset=".54" stopColor="#00a9dc" /><stop offset=".83" stopColor="#0861b1" /><stop offset="1" stopColor="#053b7d" />
        </radialGradient>
        <radialGradient id={`${id}-green`} cx="30%" cy="22%" r="85%">
          <stop stopColor="#efffc2" /><stop offset=".28" stopColor="#9af33b" /><stop offset=".52" stopColor="#3acb13" /><stop offset=".82" stopColor="#128333" /><stop offset="1" stopColor="#065332" />
        </radialGradient>
        <linearGradient id={`${id}-silver`} x2=".25" y2="1">
          <stop stopColor="#fff" /><stop offset=".45" stopColor="#e4faff" /><stop offset=".52" stopColor="#a4d3e0" /><stop offset=".87" stopColor="#f3ffff" /><stop offset="1" stopColor="#87b4cb" />
        </linearGradient>
        <radialGradient id={`${id}-gold`} cx="30%" cy="20%" r="90%">
          <stop stopColor="#fff5b9" /><stop offset=".4" stopColor="#ffda55" /><stop offset=".72" stopColor="#e9a31d" /><stop offset="1" stopColor="#ad7115" />
        </radialGradient>
        <linearGradient id={`${id}-shine`} x2=".1" y2="1">
          <stop stopColor="#fff" stopOpacity=".95" /><stop offset="1" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>
      {object}
    </svg>
  );
}
