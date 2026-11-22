import { Icon } from "./Icon";

/** Decorative glass objects; no extra content for assistive technology. */
export function AeroScene({ people = false }: { people?: boolean }) {
  return (
    <div className="aero-scene" aria-hidden="true">
      <div className="aero-scene__orbit aero-scene__orbit--outer" />
      <div className="aero-scene__orbit aero-scene__orbit--inner" />
      <span className="aero-scene__bubble aero-scene__bubble--one" />
      <span className="aero-scene__bubble aero-scene__bubble--two" />
      <span className="aero-scene__bubble aero-scene__bubble--three" />
      <div className="aero-scene__satellite aero-scene__satellite--left">
        <Icon name={people ? "shield" : "users"} />
      </div>
      <div className="aero-scene__main">
        <Icon name={people ? "users" : "database"} />
      </div>
      <div className="aero-scene__satellite aero-scene__satellite--right">
        <Icon name={people ? "key" : "layers"} />
      </div>
      <span className="aero-scene__ripple" />
    </div>
  );
}
