from enum import Enum
from typing import Protocol
"""Fixture exercising inheritance, decorators, nesting and async."""

MAX_ANIMALS = 100
default_name = "rex"


class Animal:
    """Base animal.

    Second paragraph, not extracted.
    """

    kingdom = "animalia"
    count: int = 0

    def __init__(self, name):
        self.name = name
        self.legs: int = 4

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


class Drawable(Protocol):
    def draw(self): ...


class Color(Enum):
    RED = 1
    GREEN = 2
