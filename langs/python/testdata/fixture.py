"""Fixture exercising inheritance, decorators, nesting and async."""

MAX_ANIMALS = 100
default_name = "rex"


class Animal:
    """Base animal.

    Second paragraph, not extracted.
    """

    def __init__(self, name):
        self.name = name

    def speak(self):
        """Make a sound."""
        raise NotImplementedError


class Dog(Animal):
    def speak(self):
        return "woof"

    @property
    def tricks(self):
        return ["fetch"]

    @staticmethod
    def species():
        return "canis"

    class Puppy:
        """A nested class."""

        def nap(self):
            pass


class Puppy(Dog, Animal):
    pass


async def feed(animal):
    """Feed an animal, asynchronously."""
    await animal.eat()


def make_sound(a):
    return a.speak()


def outer():
    def inner():
        return 1

    return inner()
