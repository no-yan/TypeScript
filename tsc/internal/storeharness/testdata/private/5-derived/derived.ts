class Base {
    #x = 1;
    getBaseX() { return this.#x; }
    static baseHas(o: object) { return #x in o; }
}
class Derived extends Base {
    #x = "derived";
    getDerivedX() { return this.#x; }
    static derivedHas(o: object) { return #x in o; }
}
const d = new Derived();
const bx = d.getBaseX();
const dx = d.getDerivedX();
