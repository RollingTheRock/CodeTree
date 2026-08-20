package zoo;

interface Drawable {
    void draw();
}

interface Named {
    String name();
}

interface Entity extends Drawable, Named {
}

abstract class Animal implements Entity {
    protected String name;
    int legs = 4;

    Animal(String name) {
        this.name = name;
    }

    abstract void speak();

    class Collar {
        String color;
    }
}

class Dog extends Animal implements Comparable<Dog> {
    private int tricks;

    Dog(String name) {
        super(name);
    }

    @Override
    void speak() {
    }

    static Dog create(String name) {
        return new Dog(name);
    }
}

enum Color {
    RED, GREEN
}

record Point(int x, int y) {
}
